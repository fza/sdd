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

func TestConfigSetJudgesTheDialogueModeValue(t *testing.T) {
	cases := []struct {
		name    string
		target  string
		value   string
		refused bool
	}{
		{name: "local autonomous", target: "local", value: "autonomous"},
		{name: "global guided", target: "global", value: "guided"},
		{name: "local unknown value", target: "local", value: "free", refused: true},
		{name: "global wrong case", target: "global", value: "Autonomous", refused: true},
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

			err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: tc.target, Key: "dialogue.mode", Value: tc.value})
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), "dialogue.mode") {
					t.Errorf("a value other than guided or autonomous should be refused naming the key, got %v", err)
				}
				return
			}
			if err != nil {
				t.Errorf("guided and autonomous should be written: %v", err)
			}
		})
	}
}
