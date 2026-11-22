package mcpapp_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// readEvents returns the logged read events of a tool, in log order.
func readEvents(t *testing.T, env testEnv, session, tool string) []engine.Event {
	t.Helper()
	events, err := readSessionLog(t, env.sessionsDir, session)
	if err != nil {
		t.Fatal(err)
	}
	var reads []engine.Event
	for _, event := range events {
		if event.Event == engine.EventRead && event.Data["tool"] == tool {
			reads = append(reads, event)
		}
	}
	return reads
}

func TestViewLogsItsReads(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	var listed mcpserver.ViewResult
	call(t, cs, "view", map[string]any{"session": session, "layout": "active:as-list"}, &listed)
	reads := readEvents(t, env, session, "view")
	if len(reads) != 1 {
		t.Fatalf("a list view should log one read event, got %d", len(reads))
	}
	if summary, _ := reads[0].Data["summary"].([]any); len(summary) == 0 {
		t.Fatalf("a list view should log its rows as summary reads: %+v", reads[0].Data)
	}
	if _, full := reads[0].Data["full"]; full {
		t.Fatalf("a list view serves no body in full: %+v", reads[0].Data)
	}

	var bodies mcpserver.ViewResult
	call(t, cs, "view", map[string]any{"session": session, "layout": "active:n(1):as-bodies"}, &bodies)
	reads = readEvents(t, env, session, "view")
	if len(reads) != 2 {
		t.Fatalf("a second view should log a second read event, got %d", len(reads))
	}
	if full, _ := reads[1].Data["full"].([]any); len(full) != 1 {
		t.Fatalf("a bodies view should log its entry as a full read: %+v", reads[1].Data)
	}
}

// An entry gather served in full serves in full again on a later gather: a
// read carries no answer that could hold it (20261121-230157-d-tac-ym8).
func TestGatherServesAnEntryInFullAgainOnALaterCall(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	for range 2 {
		var opened mcpserver.GatherResult
		call(t, cs, "gather", map[string]any{"session": session, "ids": []string{fixtureGapID}}, &opened)
		if !strings.Contains(opened.Serve, "# relevant") || !strings.Contains(opened.Serve, "id: "+fixtureGapID) || strings.Contains(opened.Serve, pointerMark) {
			t.Fatalf("gather with ids should serve the entry in full on every call:\n%s", opened.Serve)
		}
	}
}

// A gather paged past the result bound carries candidate lines on its first
// page, not the to-judge heading alone.
func TestGatherFirstPageCarriesCandidatesToJudge(t *testing.T) {
	graphDir := writeFixtureGraph(t)
	for n := range 60 {
		summary := fmt.Sprintf("Gap %d of the paging fixture is a candidate whose brief line runs long enough that sixty of them fill more than one gather result.", n)
		entry := strings.Replace(gapEntry, "summary: Pre-flight verdict oscillation produces contradictory findings across runs.", "summary: "+summary, 1)
		path := filepath.Join(graphDir, fmt.Sprintf("2026/06/02-%06d-s-tac-p%02d.md", 100000+n, n))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(entry), 0644); err != nil {
			t.Fatal(err)
		}
	}
	env := newTestServer(t, nil, graphDir, "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	var first mcpserver.GatherResult
	call(t, cs, "gather", map[string]any{"session": session}, &first)
	if !strings.Contains(first.Serve, "(page 1 of ") {
		t.Fatalf("the fixture's gather should page past the result bound:\n%.400s", first.Serve)
	}
	_, toJudge, ok := strings.Cut(first.Serve, "# candidates (")
	if !ok {
		t.Fatalf("the first page should open the candidates section:\n%.400s", first.Serve)
	}
	if !strings.Contains(toJudge, "\n- ") {
		t.Fatalf("the first page should carry candidates to judge, got:\n%s", toJudge)
	}
}

func TestGatherCreditsTheNamedCapture(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &serve)

	var served mcpserver.GatherResult
	call(t, cs, "gather", map[string]any{"session": session, "instance": serve.Instance}, &served)
	reads := readEvents(t, env, session, "gather")
	if len(reads) != 1 || reads[0].Instance != serve.Instance {
		t.Fatalf("gather should log one read credited to the capture: %+v", reads)
	}
	if _, ok := reads[0].Data["grounding"]; !ok {
		t.Fatalf("the credited read should carry the reads as run: %+v", reads[0].Data)
	}

	if msg := callExpectError(t, cs, "gather", map[string]any{"session": session, "instance": "i_99"}); !strings.Contains(msg, "i_99") {
		t.Fatalf("gather naming an unknown instance should be refused naming it: %s", msg)
	}

	callRaw(t, cs, "abandon", map[string]any{"session": session, "instance": serve.Instance, "reason": "test teardown"})
	if msg := callExpectError(t, cs, "gather", map[string]any{"session": session, "instance": serve.Instance}); !strings.Contains(msg, "running") {
		t.Fatalf("gather naming an ended capture should be refused: %s", msg)
	}
	if reads := readEvents(t, env, session, "gather"); len(reads) != 1 {
		t.Fatalf("a refused gather should log no read: %+v", reads)
	}
}

// A view or show paged past the result bound logs its bodies as full reads
// only once every page has served: refsInspected must not pass on a body the
// agent never got.
func TestAPagedReadLogsItsBodiesOnlyOnceEveryPageServed(t *testing.T) {
	dir, ids, _ := largeGraph(t)
	for _, tc := range []struct {
		tool  string
		field string
		args  map[string]any
	}{
		{tool: "view", field: "sections", args: map[string]any{"layout": "active:n(12):as-bodies"}},
		{tool: "show", field: "entries", args: map[string]any{"ids": ids[:12]}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			env := newTestServer(t, nil, dir, "")
			cs := connect(t, env.srv)
			tc.args["session"] = openSession(t, cs).Session
			text := func(result map[string]any) string { s, _ := result[tc.field].(string); return s }

			page := callRaw(t, cs, tc.tool, tc.args)
			if !strings.Contains(text(page), "for the next;") {
				t.Fatalf("the read should page past the result bound, got %.200q", text(page))
			}
			full := func() []any {
				var listed []any
				for _, read := range readEvents(t, env, tc.args["session"].(string), tc.tool) {
					ids, _ := read.Data["full"].([]any)
					listed = append(listed, ids...)
				}
				return listed
			}
			if listed := full(); len(listed) > 0 {
				t.Fatalf("a body on a page not served yet should not count as read in full: %v", listed)
			}

			for n := 2; strings.Contains(text(page), "for the next;"); n++ {
				tc.args["page"] = n
				page = callRaw(t, cs, tc.tool, tc.args)
			}
			if listed := full(); len(listed) != 12 {
				t.Fatalf("every body should count as read in full once its last page served, got %d: %v", len(listed), listed)
			}
		})
	}
}
