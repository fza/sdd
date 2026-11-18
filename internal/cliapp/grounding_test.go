package cliapp_test

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCLIGroundingStatusNeedsAJudge(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: false\n")
	result := f.run(t, "grounding", "status")
	if result.err == nil || !strings.Contains(result.err.Error(), "no prefilter judge") {
		t.Fatalf("sdd grounding status without a prefilter = %v, should name the missing judge", result.err)
	}
}

func TestCLIGroundingStatusReportsTheConfiguredJudge(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: true\n")
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm_prefilter:\n  protocol: systemone\n  api_keys:\n    opper: test-key\n")
	result := f.run(t, "grounding", "status")
	if result.err != nil {
		t.Fatalf("sdd grounding status: %v\n%s", result.err, result.stderr)
	}
	assertContainsOutput(t, "stdout", result.stdout, "judge: systemone:jev-1.14.0", "thresholds: none yet")
}

// Without an llm_prefilter block the prefilter is on and judges with generate
// on the llm provider and model.
func TestCLIGroundingStatusDerivesTheJudgeFromLLM(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm:\n  provider: codex\n  model: gpt-5.5\n")
	result := f.run(t, "grounding", "status")
	if result.err != nil {
		t.Fatalf("sdd grounding status: %v\n%s", result.err, result.stderr)
	}
	assertContainsOutput(t, "stdout", result.stdout, "judge: generate:gpt-5.5")
}

// While the recall bound does not hold, the status says the prefilter is
// uncalibrated and names the command that seeds it.
func TestCLIGroundingStatusNamesTheSeedWhileUncalibrated(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: true\n")
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm_prefilter:\n  protocol: systemone\n  api_keys:\n    opper: test-key\n")
	result := f.run(t, "grounding", "status")
	if result.err != nil {
		t.Fatalf("sdd grounding status: %v\n%s", result.err, result.stderr)
	}
	assertContainsOutput(t, "stdout", result.stdout, "uncalibrated:", "sdd grounding seed --from-refs")
}

// serve logs once at its start that the prefilter is uncalibrated, naming
// the command that seeds it; without a judge it logs nothing of it.
func TestServeLogsAnUncalibratedPrefilterOnce(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: false\n")
	if stderr := f.serveArgs(t, nil, nil, func(*mcp.ClientSession) {}); strings.Contains(stderr, "uncalibrated") {
		t.Fatalf("serve without a prefilter judge should not warn of its calibration:\n%s", stderr)
	}
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: true\n")
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm_prefilter:\n  protocol: systemone\n  api_keys:\n    opper: test-key\n")
	stderr := f.serveArgs(t, nil, nil, func(*mcp.ClientSession) {})
	if strings.Count(stderr, "uncalibrated") != 1 || !strings.Contains(stderr, "sdd grounding seed --from-refs") {
		t.Fatalf("serve should warn once of the uncalibrated prefilter and name the seed:\n%s", stderr)
	}
}

func TestCLIGroundingSeedTakesOneSource(t *testing.T) {
	f := newAppFixture(t)
	for _, args := range [][]string{{"grounding", "seed"}, {"grounding", "seed", "--from", "-", "--from-refs"}} {
		result := f.run(t, args...)
		if result.err == nil || !strings.Contains(result.err.Error(), "--from or --from-refs") {
			t.Errorf("sdd %v = %v, should ask for exactly one of --from and --from-refs", args, result.err)
		}
	}
}

func TestCLIGroundingSeedFromRefsNeedsAJudge(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: false\n")
	result := f.run(t, "grounding", "seed", "--from-refs")
	if result.err == nil || !strings.Contains(result.err.Error(), "no prefilter judge") {
		t.Fatalf("sdd grounding seed --from-refs without a prefilter = %v, should name the missing judge", result.err)
	}
}
