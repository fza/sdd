package handlers_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

func configRepoHandler(t *testing.T) (*handlers.Handler, string, string) {
	t.Helper()
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "xdg", "sdd", "config.yaml")
	sddDir := filepath.Join(dir, "repo", ".sdd")
	err := os.MkdirAll(sddDir, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	registry := repos.NewRegistry(repos.Locations{ConfigPath: globalPath, CacheRoot: filepath.Join(dir, "cache")})
	h := handlers.New(handlers.Options{SDDDir: sddDir, Repos: repos.NewManager(registry, git.CLI{}, nil)})
	return h, globalPath, sddDir
}

func TestConfigSetRepoOverride(t *testing.T) {
	for _, target := range []string{"global", "local"} {
		t.Run(target, func(t *testing.T) {
			h, globalPath, sddDir := configRepoHandler(t)
			err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: target, Key: "connected.write", Value: "true", Repo: "github.com/acme/platform-sdd"})
			if err != nil {
				t.Fatalf("ConfigSet: %v", err)
			}
			path := globalPath
			if target == "local" {
				path = meta.LocalConfigPath(sddDir, "")
			}
			written, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := model.ParseConfig(written)
			if err != nil {
				t.Fatal(err)
			}
			override := cfg.Connected.Repos["github.com/acme/platform-sdd"]
			if override.Write == nil || !*override.Write {
				t.Errorf("the override entry should hold write true:\n%s", written)
			}
			if cfg.Connected.Write != nil {
				t.Errorf("the catch-all should stay unset:\n%s", written)
			}
		})
	}
}

func TestConfigSetRefusesInbound(t *testing.T) {
	for _, tc := range []struct{ target, key, repo string }{
		{target: "global", key: "inbound.accept"},
		{target: "local", key: "inbound.accept"},
		{target: "local", key: "inbound.accept", repo: "github.com/acme/app"},
	} {
		t.Run(tc.target+" "+tc.repo, func(t *testing.T) {
			h, globalPath, sddDir := configRepoHandler(t)
			err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: tc.target, Key: tc.key, Value: "true", Repo: tc.repo})
			if err == nil || !strings.Contains(err.Error(), "committed .sdd/config.yaml") {
				t.Fatalf("inbound outside the committed config should be refused, got %v", err)
			}
			for _, path := range []string{globalPath, meta.LocalConfigPath(sddDir, "")} {
				_, statErr := os.Stat(path)
				if statErr == nil {
					t.Errorf("a refused write should leave no file behind: %s", path)
				}
			}
		})
	}
}
