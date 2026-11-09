package meta_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// verify.mode layers like every policy key (20261109-004506-d-cpt-udz, D2):
// the global and committed configs set it with the committed one winning; the
// machine-local config may only raise it.
func TestVerifyModeLayers(t *testing.T) {
	cases := []struct {
		name      string
		global    model.VerifyMode
		committed string
		local     string
		enforce   bool
		verifier  model.VerifierMode
		refused   string
	}{
		{name: "unset stays off", verifier: model.VerifierOff},
		{name: "global grounding", global: model.VerifyGrounding, enforce: true, verifier: model.VerifierOff},
		{name: "committed wins over global", global: model.VerifyDeep, committed: "verify:\n  mode: off\n", verifier: model.VerifierOff},
		{name: "local may raise it", committed: "verify:\n  mode: off\n", local: "verify:\n  mode: grounding\n", enforce: true, verifier: model.VerifierOff},
		{name: "local deep over committed prose", committed: "verify:\n  mode: prose\n", local: "verify:\n  mode: deep\n", enforce: true, verifier: model.VerifierDeep},
		{name: "local off under global grounding is refused", global: model.VerifyGrounding, local: "verify:\n  mode: off\n", refused: "verify.mode off is refused in the machine-local config"},
		{name: "local grounding under committed prose is refused", committed: "verify:\n  mode: prose\n", local: "verify:\n  mode: grounding\n", refused: "verify.mode grounding is refused in the machine-local config"},
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
			global := model.BaseConfig{Verify: model.VerifyConfig{Mode: tc.global}}
			cfg, err := meta.ResolveConfig(global, sddDir, "")
			if tc.refused != "" {
				if err == nil || !strings.Contains(err.Error(), tc.refused) || !strings.Contains(err.Error(), filepath.Join(sddDir, meta.LocalConfigFileName)) {
					t.Fatalf("a machine-local layer lowering the mode should be refused naming the key and the file, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var block model.VerifyConfig
			if cfg != nil {
				block = cfg.Verify
			}
			if got := block.GroundingPolicy().Enforce; got != tc.enforce {
				t.Errorf("enforcement resolved wrong: got %v", got)
			}
			if got := block.VerifierPolicy().Mode; got != tc.verifier {
				t.Errorf("verifier mode resolved wrong: got %s", got)
			}
		})
	}
}
