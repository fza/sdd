package mcpapp_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
	"github.com/networkteam/sdd/test"
)

// writeShapedGraph writes the realistic graph of shape, its base just before
// the test server's clock.
func writeShapedGraph(t *testing.T, shape proctest.GraphShape) string {
	t.Helper()
	graphDir := filepath.Join(t.TempDir(), "graph")
	base := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	for _, entry := range proctest.RealisticGraph(base, shape) {
		proctest.WriteEntry(t, graphDir, entry)
	}
	proctest.WriteWIPMarkers(t, graphDir, base, shape)
	return graphDir
}

// shapedServer serves the realistic graph of shape.
func shapedServer(shape proctest.GraphShape) func(*testing.T) *mcpserver.Server {
	return func(t *testing.T) *mcpserver.Server { return newTestServer(t, nil, writeShapedGraph(t, shape), "").srv }
}

// growingProjectShape is the young project's graph with the aspirations and
// directives of a project some months on.
var growingProjectShape = func() proctest.GraphShape {
	shape := youngProjectShape
	shape.Aspirations, shape.GuidingDirectives, shape.Focuses = 6, 6, 2
	return shape
}()

const sizeBoundWait = "as this result would run past its size bound"

// Both doors serve within a serve's share of the result bound to an agent
// that lists no proofs, on graphs from a young project's to a mature one's
// and on a German one: the opening counts the proof guide inside the bound,
// and what does not fit, framing, lanes and the vocabulary table alike, waits
// and serves on the shell's next calls, which send nothing again.
func TestTheDoorsServeWithinTheResultBound(t *testing.T) {
	graphs := map[string]struct {
		server  func(*testing.T) *mcpserver.Server
		phrases []string
	}{
		"young project":   {shapedServer(youngProjectShape), []string{"## Working principles", "Local participant: Tester"}},
		"growing project": {shapedServer(growingProjectShape), []string{"## Working principles", "Local participant: Tester"}},
		"mature project":  {shapedServer(proctest.DefaultShape()), []string{"## Working principles", "Local participant: Tester"}},
		"German project":  {func(t *testing.T) *mcpserver.Server { return newTestServerConfig(t, nil, "", "", "de").srv }, []string{"Vokabular", "Language: de"}},
	}
	for graph, tc := range graphs {
		phrases := append(tc.phrases, "Ending is part of the arc")
		for _, door := range []string{"start_session", "resume_session"} {
			t.Run(graph+"/"+door, func(t *testing.T) {
				cs, bound := boundedConnect(t, tc.server(t), false)
				defer func() { t.Logf("largest results: %v", bound.largest) }()
				opening := openSession(t, cs)
				if size := jsonSize(t, opening); size > model.MaxServeChars {
					t.Fatalf("the opening, its proof guide included, should cost at most %d characters, got %d", model.MaxServeChars, size)
				}
				if !strings.Contains(opening.Instructions, "list in held every tag") {
					t.Fatalf("the opening should carry the proof guide, got %.300q", opening.Instructions)
				}
				instructions, text := opening.Instructions, opening.Instructions+"\n\n"+opening.Framing+"\n\n"+opening.Vocabulary
				if door == "resume_session" {
					var resumed mcpserver.ResumeSessionResult
					call(t, cs, "resume_session", map[string]any{"session": opening.Session}, &resumed)
					if size := jsonSize(t, resumed); size > model.MaxServeChars {
						t.Fatalf("the resume should cost at most %d characters, got %d", model.MaxServeChars, size)
					}
					instructions, text = resumed.Instructions, resumed.Instructions+"\n\n"+resumed.Framing
					for _, open := range resumed.Open {
						instructions += "\n\n" + open.Instructions
						text += "\n\n" + open.Instructions + "\n\n" + open.Vocabulary
					}
				}
				text = test.Untagged(text)
				for calls := 0; strings.Contains(instructions, sizeBoundWait); calls++ {
					if calls == 3 {
						t.Fatalf("everything waiting should reach the agent within 3 more calls, still waiting:\n%s", instructions)
					}
					var serve mcpserver.ServeResult
					call(t, cs, "next", map[string]any{"session": opening.Session, "instance": opening.Instance}, &serve)
					if serve.Step != opening.Step {
						t.Fatalf("the shell should stay at %s, got %q", opening.Step, serve.Step)
					}
					followUp := test.Untagged(serve.Instructions + "\n\n" + serve.Framing + "\n\n" + serve.Vocabulary)
					for _, phrase := range phrases {
						if strings.Contains(text, phrase) && strings.Contains(followUp, phrase) {
							t.Fatalf("call %d should point at what the calls before sent, not send %q again", calls+1, phrase)
						}
					}
					instructions, text = serve.Instructions, text+"\n\n"+followUp
				}
				for _, phrase := range phrases {
					if !strings.Contains(text, phrase) {
						t.Errorf("the results should carry %q", phrase)
					}
				}
			})
		}
	}
}
