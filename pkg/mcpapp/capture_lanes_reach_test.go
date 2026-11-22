package mcpapp_test

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/proctest"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

var waitingLanes = regexp.MustCompile(`more lanes of these instructions wait[^:]*: ([^.]*)\.`)

var heldIntroLane = regexp.MustCompile(`\(held: [^)]*lane intro`)

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

// An agent that answers lost reaches every lane of the shipped capture's
// assemble step within three calls, by plain next calls and by reports that
// leave the step where it is; each follow-up points at the lanes sent before.
func TestALostAgentReachesEveryCaptureLane(t *testing.T) {
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
			text := serve.Instructions
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
				if !heldIntroLane.MatchString(serve.Instructions) {
					t.Fatalf("call %d should point at the intro lane sent before, got %.300q", calls+1, serve.Instructions)
				}
				text += "\n\n" + serve.Instructions
			}
			for _, phrase := range []string{"A signal records something noticed:", "Words the user gives for a decision's reason go to `reasonWords`", "**Topics** (`topics`)", "**Participants** (`participants`)", "**Intent** (`intent`)"} {
				if !strings.Contains(text, phrase) {
					t.Errorf("the lanes served should carry %q", phrase)
				}
			}
		})
	}
}

// Whatever an agent answers or rereads, plain next calls serve each waiting
// lane of the shipped capture's assemble step, one call per waiting lane at
// most, and the step then takes a report.
func TestWaitingCaptureLanesDrainWhateverTheAgentAnswers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers bool
		agent   func(args map[string]any, pending []string)
	}{
		{"lost", false, func(map[string]any, []string) {}},
		{"a wrong answer", false, func(args map[string]any, _ []string) { args["answer"] = "misread words" }},
		{"a right answer", true, func(map[string]any, []string) {}},
		{"a reread of every lane", false, func(args map[string]any, pending []string) {
			reread := []any{"lane intro"}
			for _, name := range pending {
				reread = append(reread, "lane "+name)
			}
			args["reread"] = reread
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, _ := boundedConnect(t, newTestServer(t, nil, writeYoungProjectGraph(t), "").srv, tc.answers)
			session := openSession(t, cs).Session
			var serve mcpserver.ServeResult
			call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture", "params": map[string]any{"kind": "directive"}}, &serve)
			waiting := waitingLanes.FindStringSubmatch(serve.Instructions)
			if waiting == nil {
				t.Fatalf("the assemble serve should leave lanes waiting, got %.300q", serve.Instructions)
			}
			pending := strings.Split(waiting[1], ", ")
			text := serve.Instructions
			for calls := 1; waitingLanes.MatchString(serve.Instructions); calls++ {
				if calls > len(pending) {
					t.Fatalf("the %d waiting lanes should serve within as many calls, still waiting: %s", len(pending), waitingLanes.FindStringSubmatch(serve.Instructions)[1])
				}
				args := map[string]any{"session": session, "instance": serve.Instance}
				tc.agent(args, pending)
				call(t, cs, "next", args, &serve)
				text += "\n\n" + serve.Instructions
			}
			for _, phrase := range []string{"**Topics** (`topics`)", "**Participants** (`participants`)", "**Intent** (`intent`)"} {
				if !strings.Contains(text, phrase) {
					t.Errorf("the lanes served should carry %q", phrase)
				}
			}
			call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance, "report": map[string]any{"confidence": "high"}}, &serve)
			if slices.Contains(serve.Missing, "confidence") {
				t.Errorf("the step should take the reported confidence, still missing %v", serve.Missing)
			}
		})
	}
}
