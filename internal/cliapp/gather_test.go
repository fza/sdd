package cliapp_test

import (
	"strings"
	"testing"
)

// The CLI credits no capture and never points: the same gather prints the
// same whole serve every time (20261109-004506-d-cpt-udz, R14).
func TestCLIGatherServesInFullEveryTime(t *testing.T) {
	f := newAppFixture(t)
	for range 2 {
		result := f.run(t, "gather", appEntryID)
		if result.err != nil {
			t.Fatalf("sdd gather: %v\n%s", result.err, result.stderr)
		}
		assertContainsOutput(t, "stdout", result.stdout, "every live brief, no search", "# relevant", "id: "+appEntryID, appBody)
		if strings.Contains(result.stdout, "served in full above") {
			t.Fatalf("the CLI printed a pointer:\n%s", result.stdout)
		}
	}
}
