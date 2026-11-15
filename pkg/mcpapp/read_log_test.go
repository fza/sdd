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
