package mcpapp_test

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/proctest"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
	"github.com/networkteam/sdd/test"
)

var waitingLanes = regexp.MustCompile(`more lanes of these instructions wait[^:]*: ([^.]*)\.`)

// youngProjectShape is the realistic fixture at the scale of a young
// project.
var youngProjectShape = proctest.GraphShape{Entries: 24, Facts: 4, Actors: 2, RolesPerActor: 1, Focuses: 1, InvolvementsPerFocus: 2, Aspirations: 1, GuidingDirectives: 2, WIPMarkers: 1, HubFanOut: 6, TopicLabels: 10}

// writeYoungProjectGraph writes the young project's graph, its base just
// before the test server's clock.
func writeYoungProjectGraph(t *testing.T) string {
	t.Helper()
	graphDir := filepath.Join(t.TempDir(), "graph")
	base := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	for _, entry := range proctest.RealisticGraph(base, youngProjectShape) {
		proctest.WriteEntry(t, graphDir, entry)
	}
	proctest.WriteWIPMarkers(t, graphDir, base, youngProjectShape)
	return graphDir
}

// An agent that lists no proofs reaches every lane of the shipped capture's
// assemble step within three calls, by plain next calls and by reports that
// leave the step where it is; each follow-up points at the lanes sent before.
func TestANoProofAgentReachesEveryCaptureLane(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report map[string]any
	}{
		{"plain next", nil},
		{"report keeping the step", map[string]any{"confidence": "high"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, _ := boundedConnect(t, newTestServer(t, nil, writeYoungProjectGraph(t), "").srv, false)
			session := openSession(t, cs).Session
			var serve mcpserver.ServeResult
			call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture", "params": map[string]any{"kind": "directive"}}, &serve)
			if !waitingLanes.MatchString(serve.Instructions) {
				t.Fatalf("the assemble serve should leave lanes waiting, got %.300q", serve.Instructions)
			}
			text := test.Untagged(serve.Instructions)
			for calls := 1; waitingLanes.MatchString(serve.Instructions); calls++ {
				if calls == 3 {
					t.Fatalf("every capture lane should reach the agent within 3 calls, still waiting: %s", waitingLanes.FindStringSubmatch(serve.Instructions)[1])
				}
				args := map[string]any{"session": session, "instance": serve.Instance}
				if tc.report != nil {
					args["report"] = tc.report
				}
				call(t, cs, "next", args, &serve)
				if serve.Step != "assemble" {
					t.Fatalf("the step should stay at assemble, got %q", serve.Step)
				}
				if !strings.Contains(serve.Instructions, "(held: lane intro") {
					t.Fatalf("call %d should point at the intro lane sent before, got %.300q", calls+1, serve.Instructions)
				}
				text += "\n\n" + test.Untagged(serve.Instructions)
			}
			for _, phrase := range []string{"A signal records something noticed:", "Ask the user for it before drafting", "**Topics** (`topics`)", "**Participants** (`participants`)", "**Intent** (`intent`)"} {
				if !strings.Contains(text, phrase) {
					t.Errorf("the lanes served should carry %q", phrase)
				}
			}
		})
	}
}
