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

func TestConfigSetRefusesTheRetiredTokenSaving(t *testing.T) {
	for _, target := range []string{"global", "local"} {
		t.Run(target, func(t *testing.T) {
			dir := t.TempDir()
			sddDir := filepath.Join(dir, ".sdd")
			if err := os.MkdirAll(sddDir, 0o755); err != nil {
				t.Fatal(err)
			}
			reg := repos.NewRegistry(repos.Locations{ConfigPath: filepath.Join(dir, "global.yaml"), CacheRoot: filepath.Join(dir, "cache")})
			h := handlers.New(handlers.Options{SDDDir: sddDir, Repos: repos.NewManager(reg, nil)})

			err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: target, Key: "sessions.token_saving", Value: "off"})
			if err == nil || !strings.Contains(err.Error(), "no longer used") {
				t.Errorf("the retired key should be refused as no longer used, got %v", err)
			}
		})
	}
}
