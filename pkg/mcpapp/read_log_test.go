package mcpapp_test

import (
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

// A view cut for size may have dropped any body past the cut, so none of its
// bodies count as read in full.
func TestViewCutForSizeLogsNoFullRead(t *testing.T) {
	graphDir := writeFixtureGraph(t)
	body := strings.Repeat("A long paragraph of the fixture that fills the view past its size bound. ", 350)
	for _, id := range []string{"s-tac-bg1", "s-tac-bg2", "s-tac-bg3"} {
		entry := strings.Replace(gapEntry, "Pre-flight verdict oscillation produces contradictory findings across runs on identical input.", body, 1)
		path := filepath.Join(graphDir, "2026/06/02-100000-"+id+".md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env := newTestServer(t, nil, graphDir, "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	var cut mcpserver.ViewResult
	call(t, cs, "view", map[string]any{"session": session, "layout": "active:as-bodies"}, &cut)
	reads := readEvents(t, env, session, "view")
	if len(reads) != 1 {
		t.Fatalf("the view should log one read event, got %d", len(reads))
	}
	if full, ok := reads[0].Data["full"]; ok {
		t.Fatalf("a view cut for size should log no full read, got %v", full)
	}
	if summary, _ := reads[0].Data["summary"].([]any); len(summary) == 0 {
		t.Fatalf("a view cut for size should log its entries as summary reads: %+v", reads[0].Data)
	}
}

func TestGatherPointsAtAnEntryAlreadyServedInFull(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session

	var opened mcpserver.GatherResult
	call(t, cs, "gather", map[string]any{"session": session, "ids": []string{fixtureGapID}}, &opened)
	if !strings.Contains(opened.Serve, "# relevant") || !strings.Contains(opened.Serve, "id: "+fixtureGapID) {
		t.Fatalf("gather with ids should serve the entry in full:\n%s", opened.Serve)
	}

	var again mcpserver.GatherResult
	call(t, cs, "gather", map[string]any{"session": session}, &again)
	served := again.Serve
	if !strings.Contains(served, fixtureGapID+" — ") || !strings.Contains(served, "(held as ⟦") {
		t.Fatalf("a later serve should point at the entry served in full:\n%s", served)
	}
	if !strings.Contains(served, "its tag left out of held") {
		t.Fatalf("the pointer should name the way back:\n%s", served)
	}

	var proved mcpserver.GatherResult
	call(t, cs, "gather", map[string]any{"session": session, "ids": []string{fixtureGapID}}, &proved)
	if strings.Contains(proved.Serve, "id: "+fixtureGapID) || !strings.Contains(proved.Serve, fixtureGapID+" — ") {
		t.Fatalf("gather with the id of an entry the call proves it holds should serve its pointer:\n%s", proved.Serve)
	}

	var reopened mcpserver.GatherResult
	call(t, cs, "gather", map[string]any{"session": session, "ids": []string{fixtureGapID}, "held": []any{}}, &reopened)
	if !strings.Contains(reopened.Serve, "id: "+fixtureGapID) {
		t.Fatalf("gather with the id and its tag left out of held is the way back and serves in full again:\n%s", reopened.Serve)
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
