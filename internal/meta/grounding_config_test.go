package meta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// Grounding enforcement layers asymmetrically (20261109-004506-d-cpt-udz, D2):
// the global and committed configs set it with the committed one winning; the
// machine-local config may only turn it on.
func TestGroundingEnforceLayers(t *testing.T) {
	enabled := true
	cases := []struct {
		name      string
		global    *bool
		committed string
		local     string
		want      bool
		refused   bool
	}{
		{name: "unset stays off"},
		{name: "global on", global: &enabled, want: true},
		{name: "committed wins over global", global: &enabled, committed: "grounding:\n  enforce: false\n", want: false},
		{name: "local may turn it on", committed: "grounding:\n  enforce: false\n", local: "grounding:\n  enforce: true\n", want: true},
		{name: "local false is refused", global: &enabled, local: "grounding:\n  enforce: false\n", refused: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sddDir := t.TempDir()
			if tc.committed != "" {
				err := os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte(tc.committed), 0o644)
				if err != nil {
					t.Fatal(err)
				}
			}
			if tc.local != "" {
				err := os.WriteFile(filepath.Join(sddDir, meta.LocalConfigFileName), []byte(tc.local), 0o644)
				if err != nil {
					t.Fatal(err)
				}
			}
			global := model.BaseConfig{Grounding: model.GroundingConfig{Enforce: tc.global}}
			cfg, err := meta.ResolveConfig(global, sddDir, "")
			if tc.refused {
				if err == nil {
					t.Fatal("a machine-local grounding.enforce: false should be refused")
				}
				if !strings.Contains(err.Error(), filepath.Join(sddDir, "config.yaml")) {
					t.Fatalf("the refusal should name the committed config file: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := false
			if cfg != nil {
				got = cfg.Grounding.Policy().Enforce
			}
			if got != tc.want {
				t.Fatalf("enforcement resolved wrong: got %v", got)
			}
		})
	}
}
