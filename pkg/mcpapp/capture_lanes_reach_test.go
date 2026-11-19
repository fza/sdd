package mcpapp_test

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
	"github.com/networkteam/sdd/test"
)

var waitingLanes = regexp.MustCompile(`more lanes of these instructions wait[^:]*: ([^.]*)\.`)

var heldIntroLane = regexp.MustCompile(`\(held: tag [0-9a-z]{4} for lane intro`)

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
				if !heldIntroLane.MatchString(serve.Instructions) {
					t.Fatalf("call %d should point at the intro lane sent before, got %.300q", calls+1, serve.Instructions)
				}
				text += "\n\n" + test.Untagged(serve.Instructions)
			}
			for _, phrase := range []string{"A signal records something noticed:", "Words the user gives for a decision's reason go to `reasonWords`", "**Topics** (`topics`)", "**Participants** (`participants`)", "**Intent** (`intent`)"} {
				if !strings.Contains(text, phrase) {
					t.Errorf("the lanes served should carry %q", phrase)
				}
			}
		})
	}
}

// Whatever an agent proves of the pieces served before, plain next calls
// serve each waiting lane of the shipped capture's assemble step, one call
// per waiting lane at most, and the step then takes a report.
func TestWaitingCaptureLanesDrainWhateverTheAgentProves(t *testing.T) {
	for _, tc := range []struct {
		name  string
		agent func(args map[string]any, anchors []model.Proof)
	}{
		{"no proofs", func(map[string]any, []model.Proof) {}},
		{"a wrong word for every tag", func(args map[string]any, anchors []model.Proof) {
			held := make([]any, 0, len(anchors))
			for _, anchor := range anchors {
				held = append(held, model.Proof{Tag: anchor.Tag, Word: "misread"}.String())
			}
			args["held"] = held
		}},
		{"the last result's tags alone", func(args map[string]any, anchors []model.Proof) {
			held := make([]any, 0, len(anchors))
			for _, anchor := range anchors {
				held = append(held, anchor.String())
			}
			args["held"] = held
		}},
		{"a reread of every tag", func(args map[string]any, anchors []model.Proof) {
			reread := make([]any, 0, len(anchors))
			for _, anchor := range anchors {
				reread = append(reread, anchor.Tag)
			}
			args["reread"] = reread
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, _ := boundedConnect(t, newTestServer(t, nil, writeYoungProjectGraph(t), "").srv, false)
			session := openSession(t, cs).Session
			var serve mcpserver.ServeResult
			call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture", "params": map[string]any{"kind": "directive"}}, &serve)
			waiting := waitingLanes.FindStringSubmatch(serve.Instructions)
			if waiting == nil {
				t.Fatalf("the assemble serve should leave lanes waiting, got %.300q", serve.Instructions)
			}
			pending := strings.Split(waiting[1], ", ")
			text := test.Untagged(serve.Instructions)
			for calls := 1; waitingLanes.MatchString(serve.Instructions); calls++ {
				if calls > len(pending) {
					t.Fatalf("the %d waiting lanes should serve within as many calls, still waiting: %s", len(pending), waitingLanes.FindStringSubmatch(serve.Instructions)[1])
				}
				raw, err := json.Marshal(serve)
				if err != nil {
					t.Fatal(err)
				}
				args := map[string]any{"session": session, "instance": serve.Instance}
				tc.agent(args, model.ReadAnchors(string(raw)))
				call(t, cs, "next", args, &serve)
				text += "\n\n" + test.Untagged(serve.Instructions)
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
