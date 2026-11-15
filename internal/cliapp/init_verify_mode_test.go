package cliapp_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// The answer is recorded as verify.mode in the committed config.
func TestInitRecordsTheVerifyMode(t *testing.T) {
	for _, tc := range []struct{ answer, block string }{
		{"off", "verify:\n  mode: \"off\"\n"},
		{"grounding", "verify:\n  mode: grounding\n"},
		{"prose", "verify:\n  mode: prose\n"},
		{"deep", "verify:\n  mode: deep\n"},
	} {
		answer := tc.answer
		t.Run(answer, func(t *testing.T) {
			f := initFixture(t)
			result := f.run(t, "init", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--verify-mode", answer)
			if result.err != nil {
				t.Fatalf("init: %v\n%s", result.err, result.stderr)
			}

			raw := readFile(t, filepath.Join(f.root, ".sdd", "config.yaml"))
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
	f := initFixture(t)
	result := f.run(t, "init", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--verify-mode", "light")
	if result.err == nil || !strings.Contains(result.err.Error(), "--verify-mode") {
		t.Fatalf("a mode outside the set should be refused naming the flag, got %v", result.err)
	}
}

// The deep verifier's runner is personal: init records it in the local
// config, and offers only the agents supported_agents names.
func TestInitRecordsTheVerifierRunnerLocally(t *testing.T) {
	f := initFixture(t)
	result := f.run(t, "init", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--verify-mode", "deep", "--verify-runner", "claude")
	if result.err != nil {
		t.Fatalf("init: %v\n%s", result.err, result.stderr)
	}

	local, err := model.ParseConfig([]byte(readFile(t, filepath.Join(f.root, ".sdd", "config.local.yaml"))))
	if err != nil {
		t.Fatal(err)
	}
	if local.Verify.Runner != model.VerifierRunnerClaude {
		t.Fatalf("the local config should carry the runner, got %q", local.Verify.Runner)
	}
	if strings.Contains(readFile(t, filepath.Join(f.root, ".sdd", "config.yaml")), "runner") {
		t.Fatal("the committed config should not carry the personal runner")
	}
}

func TestInitRefusesARunnerTheProjectDoesNotRun(t *testing.T) {
	for _, tc := range []struct{ runner, refusal string }{
		{"driver", `invalid --verify-runner "driver": this project offers subagent, claude`},
		{"helper", `invalid --verify-runner "helper"`},
	} {
		t.Run(tc.runner, func(t *testing.T) {
			f := initFixture(t)
			result := f.run(t, "init", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--verify-mode", "deep", "--verify-runner", tc.runner)
			if result.err == nil || !strings.Contains(result.err.Error(), tc.refusal) {
				t.Fatalf("a runner outside the offered set should be refused naming the flag, got %v", result.err)
			}
		})
	}
}
