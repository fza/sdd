package handlers_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
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
		{name: "local auto", target: "local", value: "auto"},
		{name: "global guided", target: "global", value: "guided"},
		{name: "local unknown value", target: "local", value: "free", refused: true},
		{name: "local former autonomous", target: "local", value: "autonomous", refused: true},
		{name: "global wrong case", target: "global", value: "Auto", refused: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			sddDir := filepath.Join(dir, ".sdd")
			if err := os.MkdirAll(sddDir, 0o755); err != nil {
				t.Fatal(err)
			}
			reg := repos.NewRegistry(repos.Locations{ConfigPath: filepath.Join(dir, "global.yaml"), CacheRoot: filepath.Join(dir, "cache")})
			h := handlers.New(handlers.Options{SDDDir: sddDir, Repos: repos.NewManager(reg, nil)})

			err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: tc.target, Key: "dialogue.mode", Value: tc.value})
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), "dialogue.mode") || !strings.Contains(err.Error(), "guided or auto") {
					t.Errorf("a value other than guided or auto should be refused naming the key and the allowed values, got %v", err)
				}
				return
			}
			if err != nil {
				t.Errorf("guided and auto should be written: %v", err)
			}
		})
	}
}
