package cliapp_test

import (
	"strings"
	"testing"
)

// The no-repeat rule cuts only in MCP mode: the same CLI read prints in full
// every time (20261109-004506-d-cpt-udz, R14).
func TestCLIReadsServeInFullEveryTime(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"show", []string{"show", appEntryID}},
		{"view as-bodies", []string{"view", "--layout", `id("` + appEntryID + `"):as-bodies`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t)
			for range 2 {
				result := f.run(t, tc.args...)
				if result.err != nil {
					t.Fatalf("sdd %s: %v\n%s", tc.name, result.err, result.stderr)
				}
				assertContainsOutput(t, "stdout", result.stdout, appBody)
				if strings.Contains(result.stdout, "served in full above") {
					t.Fatalf("the CLI printed a pointer:\n%s", result.stdout)
				}
			}
		})
	}
}
