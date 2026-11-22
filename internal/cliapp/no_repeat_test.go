package cliapp_test

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const pointerMark = "(served in full above"

// The no-repeat rule cuts only in MCP mode: the same CLI read prints in full
// every time, with and without --session, also once the MCP session read the
// entry.
func TestCLIReadsServeInFullEveryTime(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		session bool
		want    string
	}{
		{"show", []string{"show", appEntryID}, false, appBody},
		{"view as-bodies", []string{"view", "--layout", `id("` + appEntryID + `"):as-bodies`}, false, appBody},
		{"search", []string{"search", "--term", "configured graph directory"}, false, appEntryID},
		{"gather", []string{"gather", appEntryID}, false, appBody},
		{"show in a session", []string{"show", appEntryID}, true, appBody},
		{"view as-bodies in a session", []string{"view", "--layout", `id("` + appEntryID + `"):as-bodies`}, true, appBody},
		{"search in a session", []string{"search", "--term", "configured graph directory"}, true, appEntryID},
		{"gather in a session", []string{"gather", appEntryID}, true, appBody},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t)
			args := tc.args
			if tc.session {
				args = append(args, "--session", openReadingSession(t, f, appEntryID))
			}
			for range 2 {
				result := f.run(t, args...)
				if result.err != nil {
					t.Fatalf("sdd %s: %v\n%s", tc.name, result.err, result.stderr)
				}
				assertContainsOutput(t, "stdout", result.stdout, tc.want)
				if strings.Contains(result.stdout, pointerMark) {
					t.Fatalf("the CLI printed a pointer:\n%s", result.stdout)
				}
			}
		})
	}
}

// openReadingSession starts a session over the fixture's MCP server whose
// consumer read id in full twice: an entry read in an earlier call serves in
// full again (20261121-230157-d-tac-ym8).
func openReadingSession(t *testing.T, f *appFixture, id string) string {
	t.Helper()
	var session string
	f.serve(t, func(client *mcp.ClientSession) {
		opened, served, failed := callTool(t, client, "start_session", map[string]any{})
		if failed {
			t.Fatalf("start_session: %s", served)
		}
		session = opened.Session
		for range 2 {
			_, served, failed = callTool(t, client, "show", map[string]any{"session": session, "ids": []string{id}})
			if failed {
				t.Fatalf("show: %s", served)
			}
		}
		if strings.Contains(served, pointerMark) || !strings.Contains(served, appBody) {
			t.Fatalf("a repeated MCP show should serve the entry in full again:\n%s", served)
		}
	})
	return session
}
