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

// The deep verifier's launch is personal: init records it in the local
// config, and offers only the agents supported_agents names (V-D28).
func TestInitRecordsTheVerifierLaunchLocally(t *testing.T) {
	work, _ := workRepository(t)
	runApp(t, work, "init", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--strictness", "deep", "--verifier-launch", "claude")

	local, err := model.ParseConfig([]byte(readFile(t, filepath.Join(work, ".sdd", "config.local.yaml"))))
	if err != nil {
		t.Fatal(err)
	}
	if local.Verifier.Launch != model.VerifierLaunchClaude {
		t.Fatalf("the local config should carry the launch, got %q", local.Verifier.Launch)
	}
	if strings.Contains(readFile(t, filepath.Join(work, ".sdd", "config.yaml")), "launch") {
		t.Fatal("the committed config should not carry the personal launch")
	}
}

func TestInitRefusesALaunchTheProjectDoesNotRun(t *testing.T) {
	for _, tc := range []struct{ launch, refusal string }{
		{"codex", `invalid --verifier-launch "codex": this project offers subagent, claude`},
		{"custom_driver", `invalid --verifier-launch "custom_driver": this project offers subagent, claude`},
		{"helper", `invalid --verifier-launch "helper"`},
	} {
		t.Run(tc.launch, func(t *testing.T) {
			work, _ := workRepository(t)
			_, err := runAppErr(t, work, "init", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--strictness", "deep", "--verifier-launch", tc.launch)
			if err == nil || !strings.Contains(err.Error(), tc.refusal) {
				t.Fatalf("a launch outside the offered set should be refused naming the flag, got %v", err)
			}
		})
	}
}
