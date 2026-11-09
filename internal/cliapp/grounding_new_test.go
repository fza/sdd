package cliapp_test

import (
	"strings"
	"testing"
)

// Where verify.mode enforces grounding a direct `sdd new` is refused, naming
// the key, the engine capture and the recorded bypass
// (20261109-004506-d-cpt-udz, D3).
func TestCLINewIsRefusedUnderGroundingEnforcement(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  mode: grounding\n")

	result := f.run(t, "new", "--confidence", "high", "--skip-preflight", "s", "tac", "An ungrounded signal.")
	if result.err == nil {
		t.Fatal("sdd new should be refused where verify.mode enforces grounding")
	}
	for _, want := range []string{"verify.mode grounding", "start_procedure capture", "--skip-grounding"} {
		if !strings.Contains(result.err.Error(), want) {
			t.Fatalf("the refusal should name %q: %v", want, result.err)
		}
	}

	result = f.run(t, "new", "--confidence", "high", "--skip-preflight", "--skip-grounding", "--dry-run", "s", "tac", "A bypassed signal.")
	if result.err != nil {
		t.Fatalf("--skip-grounding should pass the refusal: %v\n%s", result.err, result.stderr)
	}
}
