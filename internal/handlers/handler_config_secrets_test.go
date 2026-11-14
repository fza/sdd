package handlers_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/repos"
)

func TestConfigSetSecretRulesUpdate(t *testing.T) {
	cases := []struct {
		name    string
		target  string
		value   string
		refusal string
	}{
		{name: "daily in the global config", target: "global", value: "daily"},
		{name: "off in the global config", target: "global", value: "off"},
		{name: "a value outside the set", target: "global", value: "weekly", refusal: `"weekly" is not daily or off`},
		{name: "the local config", target: "local", value: "off", refusal: "only the user-global config may set it"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			sddDir := filepath.Join(dir, "repo", ".sdd")
			if err := os.MkdirAll(sddDir, 0o755); err != nil {
				t.Fatal(err)
			}
			registry := repos.NewRegistry(repos.Locations{ConfigPath: filepath.Join(dir, "config", "config.yaml"), CacheRoot: filepath.Join(dir, "cache")})
			h := handlers.New(handlers.Options{SDDDir: sddDir, Repos: repos.NewManager(registry, nil)})

			err := h.ConfigSet(t.Context(), &command.ConfigSetCmd{Target: tc.target, Key: "secrets.rules_update", Value: tc.value})

			if tc.refusal == "" {
				if err != nil {
					t.Errorf("the value should be written: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.refusal) {
				t.Errorf("the write should be refused saying why, got %v", err)
			}
		})
	}
}
