package cliapp_test

import (
	"strings"
	"testing"
)

const pointerMark = "(held as tag "

// The no-repeat rule cuts only in MCP mode: the same CLI read prints in full
// every time.
func TestCLIReadsServeInFullEveryTime(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"show", []string{"show", appEntryID}, appBody},
		{"view as-bodies", []string{"view", "--layout", `id("` + appEntryID + `"):as-bodies`}, appBody},
		{"search", []string{"search", "--term", "configured graph directory"}, appEntryID},
		{"gather", []string{"gather", appEntryID}, appBody},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t)
			for range 2 {
				result := f.run(t, tc.args...)
				if result.err != nil {
					t.Fatalf("sdd %s: %v\n%s", tc.name, result.err, result.stderr)
				}
				assertContainsOutput(t, "stdout", result.stdout, tc.want)
				if strings.Contains(result.stdout, pointerMark) || strings.Contains(result.stdout, "⟦") {
					t.Fatalf("the CLI printed a pointer or a tag:\n%s", result.stdout)
				}
			}
		})
	}
}
