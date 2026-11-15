package meta_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// verify.mode layers like every policy key:
// the global and committed configs set it with the committed one winning; the
// machine-local config may only raise it, and a read ignores a local value
// that would lower it, resolving the shared one.
func TestVerifyModeLayers(t *testing.T) {
	cases := []struct {
		name      string
		global    model.VerifyMode
		committed string
		local     string
		enforce   bool
		verifier  model.VerifierMode
	}{
		{name: "unset resolves to prose", enforce: true, verifier: model.VerifierProse},
		{name: "local below the default is ignored", local: "verify:\n  mode: grounding\n", enforce: true, verifier: model.VerifierProse},
		{name: "global grounding", global: model.VerifyGrounding, enforce: true, verifier: model.VerifierOff},
		{name: "committed wins over global", global: model.VerifyDeep, committed: "verify:\n  mode: off\n", verifier: model.VerifierOff},
		{name: "local may raise it", committed: "verify:\n  mode: off\n", local: "verify:\n  mode: grounding\n", enforce: true, verifier: model.VerifierOff},
		{name: "local deep over committed prose", committed: "verify:\n  mode: prose\n", local: "verify:\n  mode: deep\n", enforce: true, verifier: model.VerifierDeep},
		{name: "local off under global grounding is ignored", global: model.VerifyGrounding, local: "verify:\n  mode: off\n", enforce: true, verifier: model.VerifierOff},
		{name: "local grounding under committed prose is ignored", committed: "verify:\n  mode: prose\n", local: "verify:\n  mode: grounding\n", enforce: true, verifier: model.VerifierProse},
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
