package cliapp_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// One strictness answer sets grounding.enforce and verifier.mode together in
// the committed config (20261109-004729-d-cpt-xs5, V-D28).
func TestInitRecordsTheStrictness(t *testing.T) {
	for _, tc := range []struct {
		answer  string
		enforce bool
		mode    model.VerifierMode
	}{
		{"off", false, model.VerifierOff},
		{"grounding", true, model.VerifierOff},
		{"light", true, model.VerifierLight},
		{"deep", true, model.VerifierDeep},
	} {
		t.Run(tc.answer, func(t *testing.T) {
			work, _ := workRepository(t)
			runApp(t, work, "init", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--strictness", tc.answer)

			cfg, err := model.ParseConfig([]byte(readFile(t, filepath.Join(work, ".sdd", "config.yaml"))))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Grounding.Enforce == nil || *cfg.Grounding.Enforce != tc.enforce || cfg.Verifier.Mode != tc.mode {
				t.Fatalf("the committed config should carry the pair the answer sets, got enforce %v and mode %q", cfg.Grounding.Enforce, cfg.Verifier.Mode)
			}
		})
	}
}

func TestInitRefusesAnUnknownStrictness(t *testing.T) {
	work, _ := workRepository(t)
	_, err := runAppErr(t, work, "init", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--strictness", "strict")
	if err == nil || !strings.Contains(err.Error(), "--strictness") {
		t.Fatalf("an unknown strictness should be refused naming the flag, got %v", err)
	}
}
