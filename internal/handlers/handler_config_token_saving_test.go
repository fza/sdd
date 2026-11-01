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
	"github.com/networkteam/sdd/internal/repos"
)

func TestConfigSetJudgesTokenSaving(t *testing.T) {
	cases := []struct {
		name    string
		target  string
		value   string
		refused string
	}{
		{name: "global off", target: "global", value: "off"},
		{name: "global on", target: "global", value: "on"},
		{name: "global unknown value", target: "global", value: "false", refused: "is not on or off"},
		{name: "local off", target: "local", value: "off", refused: "only the user-global config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			sddDir := filepath.Join(dir, ".sdd")
			if err := os.MkdirAll(sddDir, 0o755); err != nil {
				t.Fatal(err)
			}
			reg := repos.NewRegistry(repos.Locations{ConfigPath: filepath.Join(dir, "global.yaml"), CacheRoot: filepath.Join(dir, "cache")})
			h := handlers.New(handlers.Options{SDDDir: sddDir, Repos: repos.NewManager(reg, git.CLI{})})

			err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: tc.target, Key: "sessions.token_saving", Value: tc.value})
			if tc.refused != "" {
				if err == nil || !strings.Contains(err.Error(), tc.refused) {
					t.Errorf("the value should be refused with its reason, got %v", err)
				}
				return
			}
			if err != nil {
				t.Errorf("on and off should be written to the global config: %v", err)
			}
		})
	}
}
