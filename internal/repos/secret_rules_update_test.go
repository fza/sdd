package repos_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

func TestLoadConfigFromChecksSecretRulesUpdate(t *testing.T) {
	cases := []struct {
		value   string
		refused bool
	}{
		{value: "daily"},
		{value: "off"},
		{value: "weekly", refused: true},
		{value: "Off", refused: true},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("secrets:\n  rules_update: "+tc.value+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			cfg, err := repos.LoadConfigFrom(path)
			if !tc.refused {
				if err != nil {
					t.Fatalf("a known value should load: %v", err)
				}
				if cfg.Secrets.RulesUpdate != model.SecretRulesUpdate(tc.value) {
					t.Errorf("the loaded value should be the one the file sets, got %q", cfg.Secrets.RulesUpdate)
				}
				return
			}
			if err == nil {
				t.Fatal("an unknown value should stop the load")
			}
			for _, part := range []string{path, "secrets.rules_update", tc.value, "daily or off"} {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("the refusal should name the file, the key, the value and the valid values, missing %q: %v", part, err)
				}
			}
		})
	}
}
