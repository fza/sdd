package cliapp_test

import (
	"strings"
	"testing"
)

// The no-repeat rule cuts only in MCP mode: the same CLI read prints in full
// every time, with and without --session (20261109-004506-d-cpt-udz, R14).
func TestCLIReadsServeInFullEveryTime(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		session bool
	}{
		{"show", []string{"show", appEntryID}, false},
		{"view as-bodies", []string{"view", "--layout", `id("` + appEntryID + `"):as-bodies`}, false},
		{"show in a session", []string{"show", appEntryID}, true},
		{"view as-bodies in a session", []string{"view", "--layout", `id("` + appEntryID + `"):as-bodies`}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t)
			args := tc.args
			if tc.session {
				args = append(args, "--session", openSession(t, f))
			}
			for range 2 {
				result := f.run(t, args...)
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
