package cliapp_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// The answer is recorded as verify.mode in the committed config
// (20261109-004729-d-cpt-xs5, V-D28).
func TestInitRecordsTheVerifyMode(t *testing.T) {
	for _, tc := range []struct{ answer, block string }{
		{"off", "verify:\n  mode: \"off\"\n"},
		{"grounding", "verify:\n  mode: grounding\n"},
		{"prose", "verify:\n  mode: prose\n"},
		{"deep", "verify:\n  mode: deep\n"},
	} {
		answer := tc.answer
		t.Run(answer, func(t *testing.T) {
			work, _ := workRepository(t)
			runApp(t, work, "init", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--verify-mode", answer)

			raw := readFile(t, filepath.Join(work, ".sdd", "config.yaml"))
			if !strings.Contains(raw, tc.block) {
				t.Fatalf("the committed config should carry the verify block with the answer:\n%s", raw)
			}
			cfg, err := model.ParseConfig([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			if string(cfg.Verify.Mode) != answer {
				t.Fatalf("the committed config should resolve to the answer, got %q", cfg.Verify.Mode)
			}
		})
	}
}

func TestInitRefusesAnUnknownVerifyMode(t *testing.T) {
	work, _ := workRepository(t)
	_, err := runAppErr(t, work, "init", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--verify-mode", "light")
	if err == nil || !strings.Contains(err.Error(), "--verify-mode") {
		t.Fatalf("a mode outside the set should be refused naming the flag, got %v", err)
	}
}

// The deep verifier's launch is personal: init records it in the local
// config, and offers only the agents supported_agents names (V-D28).
func TestInitRecordsTheVerifierLaunchLocally(t *testing.T) {
	work, _ := workRepository(t)
	runApp(t, work, "init", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--verify-mode", "deep", "--verifier-launch", "claude")

	local, err := model.ParseConfig([]byte(readFile(t, filepath.Join(work, ".sdd", "config.local.yaml"))))
	if err != nil {
		t.Fatal(err)
	}
	if local.Verify.Launch != model.VerifierLaunchClaude {
		t.Fatalf("the local config should carry the launch, got %q", local.Verify.Launch)
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
			_, err := runAppErr(t, work, "init", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--verify-mode", "deep", "--verifier-launch", tc.launch)
			if err == nil || !strings.Contains(err.Error(), tc.refusal) {
				t.Fatalf("a launch outside the offered set should be refused naming the flag, got %v", err)
			}
		})
	}
}

// The verify mode is a project setting: an init in the work repository
// records it in the project's committed config, never in the pointer.
func TestInitInTheWorkRepositoryRecordsTheProjectsVerifyMode(t *testing.T) {
	work, project := workRepository(t)
	runApp(t, work, "init", "--project-dir", "../work-sdd", "--graph-dir", "graph", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")

	runApp(t, work, "init", "--verify-mode", "deep", "--verifier-launch", "subagent")

	cfg, err := model.ParseConfig([]byte(readFile(t, filepath.Join(project, ".sdd", "config.yaml"))))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Verify.Mode != model.VerifyDeep {
		t.Fatalf("the project's committed config should carry the answer, got mode %q", cfg.Verify.Mode)
	}
	if pointer := readFile(t, filepath.Join(work, ".sdd", "config.yaml")); strings.Contains(pointer, "verify") {
		t.Errorf("the pointer should carry no project setting:\n%s", pointer)
	}
	if status := gitIn(t, project, "status", "--porcelain"); status != "" {
		t.Errorf("the project's verify mode should be committed there:\n%s", status)
	}
}
