package cliapp_test

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Where titles.backfill is on, sdd serve returns only once the title backfill
// a session start began has committed, so no write outlives the process;
// elsewhere a session start titles nothing.
func TestServeBackfillsTitlesAtSessionStartOnlyWhereSwitchedOn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		switched string
		titled   bool
	}{
		{name: "switched on", switched: titlesBackfillOn, titled: true},
		{name: "not switched on"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFillFixture(t)
			f.write(t, ".sdd/config.local.yaml", "participant: Local Author\n"+tc.switched)

			f.serve(t, func(client *mcp.ClientSession) {
				openServedSession(t, client)
			})

			subject := headSubject(t, f.root)
			if strings.HasPrefix(subject, "sdd: summarize") != tc.titled {
				t.Errorf("the backfill's commit should be in place when serve returns only with the switch on, head is %q", subject)
			}
			if strings.Contains(f.read(t, appEntryRel), "title:") != tc.titled {
				t.Error("the entry should be titled only with the switch on")
			}
		})
	}
}
