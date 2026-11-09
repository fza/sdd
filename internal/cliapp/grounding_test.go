package cliapp_test

import (
	"strings"
	"testing"
)

func TestCLIGroundingStatusNeedsAJudge(t *testing.T) {
	f := newAppFixture(t)
	result := f.run(t, "grounding", "status")
	if result.err == nil || !strings.Contains(result.err.Error(), "no prefilter judge") {
		t.Fatalf("sdd grounding status without a prefilter = %v, should name the missing judge", result.err)
	}
}

func TestCLIGroundingStatusReportsTheConfiguredJudge(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\ngrounding:\n  prefilter: true\n")
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm_prefilter:\n  protocol: systemone\n  api_keys:\n    opper: test-key\n")
	result := f.run(t, "grounding", "status")
	if result.err != nil {
		t.Fatalf("sdd grounding status: %v\n%s", result.err, result.stderr)
	}
	assertContainsOutput(t, "stdout", result.stdout, "judge: systemone:jev-1.14.0", "thresholds: none yet")
}
