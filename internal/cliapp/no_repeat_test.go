package cliapp_test

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
)

const pointerMark = "(held as ⟦"

// The no-repeat rule cuts only in MCP mode: the same CLI read prints in full
// every time, with and without --session, also once the MCP session holds the
// entry (20261109-004506-d-cpt-udz, R14).
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
				args = append(args, "--session", openHoldingSession(t, f, appEntryID))
			}
			for range 2 {
				result := f.run(t, args...)
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

// openHoldingSession starts a session over the fixture's MCP server whose
// consumer holds id in full: a second MCP show of it, proving the tag the
// first served, serves the pointer.
func openHoldingSession(t *testing.T, f *appFixture, id string) string {
	t.Helper()
	var session string
	f.serve(t, func(client *mcp.ClientSession) {
		opened, served, failed := callTool(t, client, "start_session", map[string]any{})
		if failed {
			t.Fatalf("start_session: %s", served)
		}
		session = opened.Session
		held := []string{}
		for range 2 {
			_, served, failed = callTool(t, client, "show", map[string]any{"session": session, "ids": []string{id}, "held": held})
			if failed {
				t.Fatalf("show: %s", served)
			}
			for _, proof := range model.ReadAnchors(served) {
				held = append(held, proof.String())
			}
		}
		if !strings.Contains(served, pointerMark) {
			t.Fatalf("the session should hold the entry, so a repeated MCP show serves its pointer:\n%s", served)
		}
	})
	return session
}
