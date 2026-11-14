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

func TestConfigSetRefusesTheRetiredRelevanceKeys(t *testing.T) {
	for _, target := range []string{"global", "local"} {
		for _, kv := range [][2]string{{"relevance.commit", "true"}, {"relevance.content", "reader"}} {
			t.Run(target+" "+kv[0], func(t *testing.T) {
				dir := t.TempDir()
				sddDir := filepath.Join(dir, ".sdd")
				err := os.MkdirAll(sddDir, 0o755)
				if err != nil {
					t.Fatal(err)
				}
				reg := repos.NewRegistry(repos.Locations{ConfigPath: filepath.Join(dir, "global.yaml"), CacheRoot: filepath.Join(dir, "cache")})
				h := handlers.New(handlers.Options{SDDDir: sddDir, Repos: repos.NewManager(reg, nil)})

				err = h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: target, Key: kv[0], Value: kv[1]})
				if err == nil || !strings.Contains(err.Error(), "no longer used") || !strings.Contains(err.Error(), "machine state") {
					t.Errorf("the retired key should be refused as no longer used, got %v", err)
				}
				for _, name := range []string{filepath.Join(dir, "global.yaml"), filepath.Join(sddDir, "config.local.yaml")} {
					_, err = os.Stat(name)
					if err == nil {
						t.Errorf("a refused key should write no file: %s", name)
					}
				}
			})
		}
	}
}
