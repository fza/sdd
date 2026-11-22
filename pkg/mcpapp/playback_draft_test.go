package mcpapp_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// stagedBody is a staged capture body of lines that tag says apart.
func stagedBody(tag string, repeat int) string {
	var body strings.Builder
	for i := range 9 {
		fmt.Fprintf(&body, "Paragraph %d %s: %s\n", i, tag, strings.Repeat("the playback fixture observes one more thing about the draft under load. ", repeat))
	}
	return body.String()
}

// largePlayback brings the shipped capture over a staged body and refs with
// long descriptions to its playback, then re-stages the body so the change
// waits to be played back.
func largePlayback(t *testing.T, revised string, bounded bool) (*mcp.ClientSession, string, string) {
	t.Helper()
	var entries []*model.Entry
	var refs []map[string]any
	for n := range 8 {
		e := &model.Entry{
			ID: fmt.Sprintf("20260601-%06d-s-tac-p%02d", 100000+n, n), Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
			Summary: fmt.Sprintf("Entry %d of the playback fixture observes a gap.", n),
			Content: "The playback fixture observes a gap.", Topics: proctest.MustTopics("testing/fixture"), Confidence: "medium",
		}
		entries = append(entries, e)
		refs = append(refs, map[string]any{"id": e.ID, "kind": "related", "desc": strings.Repeat(fmt.Sprintf("entry %d sits beside this gap; ", n), 12)})
	}
	world := proctest.NewWorld(t, proctest.WithEntries(entries...))
	srv, err := mcpserver.New(mcpserver.Options{Application: world.App, SearchSyncMode: sdd.SearchSyncAll, LocalIdentity: world.Identity, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	var cs *mcp.ClientSession
	if bounded {
		cs, _ = boundedConnect(t, srv, false)
	} else {
		cs = connectNamed(t, srv, "test-client")
	}
	session := openSession(t, cs).Session
	var staged mcpserver.StageAttachmentResult
	call(t, cs, "stage_attachment", map[string]any{"session": session, "files": []map[string]any{{"name": "draft.md", "content": stagedBody("as drafted", 8)}}}, &staged)
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &serve)
	capture := serve.Instance
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	callRaw(t, cs, "gather", map[string]any{"session": session, "instance": capture, "ids": ids})
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{
		"bodyHandle": staged.StagedFiles()[0].Handle, "entryKind": "gap", "layer": "tactical", "refs": refs,
		"topics": []string{"testing/fixture"}, "confidence": "medium", "widenReport": "opened every fixture entry",
	}}, &serve)
	if serve.Step != "playback" {
		t.Fatalf("the draft should reach playback, got %q: %.300q", serve.Step, serve.Instructions)
	}
	if waitingLanes.MatchString(serve.Instructions) {
		call(t, cs, "next", map[string]any{"session": session, "instance": capture}, &serve)
	}
	call(t, cs, "stage_attachment", map[string]any{"session": session, "files": []map[string]any{{"name": "draft.md", "content": revised}}}, &staged)
	return cs, session, capture
}

// requireDraftWholeAndFirst asserts a playback serve leads with the draft
// whole, while the step's other lanes wait.
func requireDraftWholeAndFirst(t *testing.T, serve mcpserver.ServeResult) {
	t.Helper()
	if !strings.HasPrefix(serve.Instructions, "Draft ") {
		t.Errorf("the playback should serve the draft first, got %.300q", serve.Instructions)
	}
	for _, want := range []string{"Paragraph 0 as revised", "Paragraph 8 as revised"} {
		if !strings.Contains(serve.Instructions, want) {
			t.Errorf("the playback should serve the draft whole, missing %q", want)
		}
	}
	if waiting := waitingLanes.FindStringSubmatch(serve.Instructions); waiting == nil || strings.Contains(waiting[1], "draft") {
		t.Errorf("the draft should serve while other lanes wait, got %q", waiting)
	}
}

// A playback whose draft would not fit beside the step's other lanes serves
// the draft whole and first, the other lanes waiting, and the confirm after
// it applies.
func TestAPlaybackServesItsDraftWholeAndFirst(t *testing.T) {
	t.Run("served by next", func(t *testing.T) {
		cs, session, capture := largePlayback(t, stagedBody("as revised", 8), true)
		var serve mcpserver.ServeResult
		call(t, cs, "next", map[string]any{"session": session, "instance": capture}, &serve)
		requireDraftWholeAndFirst(t, serve)
		call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "write it"}}, &serve)
		if serve.Step == "playback" {
			t.Fatalf("the confirm should apply, it served the playback again: %.500q", serve.Instructions)
		}
	})
	t.Run("served by a confirm over the unseen change", func(t *testing.T) {
		cs, session, capture := largePlayback(t, stagedBody("as revised", 8), true)
		var serve mcpserver.ServeResult
		call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "write it"}}, &serve)
		if serve.Step != "playback" || !strings.Contains(serve.Instructions, "draft changed since last served") {
			t.Fatalf("a confirm over the unseen change should play it back, got %q: %.300q", serve.Step, serve.Instructions)
		}
		requireDraftWholeAndFirst(t, serve)
		call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "write it"}}, &serve)
		if serve.Step == "playback" {
			t.Fatalf("the confirm after the playback should apply, it served the playback again: %.500q", serve.Instructions)
		}
	})
	t.Run("a draft past the result bound", func(t *testing.T) {
		cs, session, capture := largePlayback(t, stagedBody("as revised", 40), false)
		var serve mcpserver.ServeResult
		call(t, cs, "next", map[string]any{"session": session, "instance": capture}, &serve)
		requireDraftWholeAndFirst(t, serve)
		if model.ServedChars(serve.Instructions) <= model.DefaultResultBound.Serve() {
			t.Fatalf("the draft should run past the bound alone, the fixture too small to test: %d chars", model.ServedChars(serve.Instructions))
		}
		call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "write it"}}, &serve)
		if serve.Step == "playback" {
			t.Fatalf("the confirm should apply, it served the playback again: %.500q", serve.Instructions)
		}
	})
}
