package handlers_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

func newConfigHandler(t *testing.T) (*handlers.Handler, string, string) {
	t.Helper()
	dir := t.TempDir()
	sddDir := filepath.Join(dir, ".sdd")
	err := os.MkdirAll(sddDir, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(dir, "global.yaml")
	reg := repos.NewRegistry(repos.Locations{ConfigPath: globalPath, CacheRoot: filepath.Join(dir, "cache")})
	return handlers.New(handlers.Options{SDDDir: sddDir, Repos: repos.NewManager(reg, nil)}), globalPath, sddDir
}

func TestConfigSetRefusesRetiredSyncKeys(t *testing.T) {
	for _, key := range []string{"sync.mode", "sync.interval", "sync.cooldown"} {
		for _, target := range []string{"global", "local"} {
			t.Run(key+" "+target, func(t *testing.T) {
				h, _, _ := newConfigHandler(t)

				err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: target, Key: key, Value: "5m"})

				if err == nil || !strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "no longer used") {
					t.Errorf("a retired key should be refused naming it as no longer used, got %v", err)
				}
			})
		}
	}
}

func TestConfigSetWritesGitLimitsGlobally(t *testing.T) {
	h, globalPath, _ := newConfigHandler(t)
	ctx := context.Background()

	for _, set := range []command.ConfigSetCmd{
		{Target: "global", Key: "sync.default.fetch_cooldown", Value: "10m"},
		{Target: "global", Key: "sync.hosts.gitlab.com.retries", Value: "0"},
		{Target: "global", Key: "sync.hosts.gitlab.com.https.per_hour_unauthenticated", Value: "30"},
	} {
		err := h.ConfigSet(ctx, &set)
		if err != nil {
			t.Fatalf("%s should be written: %v", set.Key, err)
		}
	}

	cfg, err := repos.LoadConfigFrom(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	limits, warnings := model.ResolveGitLimits(cfg.Sync, "gitlab.com")
	if len(warnings) != 0 || limits.FetchCooldown.String() != "10m0s" || limits.Retries != 0 || limits.HTTPSUnauthenticatedPerHour != 30 {
		t.Errorf("the written limits should resolve for the host, got %+v %q", limits, warnings)
	}
}

func TestConfigSetRefusesGitLimits(t *testing.T) {
	cases := []struct {
		name    string
		target  string
		key     string
		value   string
		mention string
	}{
		{name: "default block in the local layer", target: "local", key: "sync.default.retries", value: "3", mention: "user-global"},
		{name: "host entry in the local layer", target: "local", key: "sync.hosts.gitlab.com.retries", value: "3", mention: "user-global"},
		{name: "fetch cooldown below the floor", target: "global", key: "sync.default.fetch_cooldown", value: "30s", mention: "minimum"},
		{name: "push cooldown below the floor", target: "global", key: "sync.hosts.gitlab.com.push_cooldown", value: "5s", mention: "minimum"},
		{name: "malformed cooldown", target: "global", key: "sync.default.push_cooldown", value: "soon", mention: "duration"},
		{name: "zero concurrency", target: "global", key: "sync.default.concurrency", value: "0", mention: "minimum"},
		{name: "not a number", target: "global", key: "sync.default.retries", value: "many", mention: "sync.default.retries"},
		{name: "host key naming no limit", target: "global", key: "sync.hosts.gitlab.com.speed", value: "1", mention: "names no git limit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, globalPath, sddDir := newConfigHandler(t)

			err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: tc.target, Key: tc.key, Value: tc.value})

			if err == nil || !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("the value should be refused saying why, got %v", err)
			}
			for _, path := range []string{globalPath, filepath.Join(sddDir, "config.local.yaml")} {
				data, readErr := os.ReadFile(path)
				if readErr == nil && strings.Contains(string(data), "sync") {
					t.Errorf("a refused value should leave %s unwritten:\n%s", path, data)
				}
			}
		})
	}
}
