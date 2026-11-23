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

// playbackBody is a capture body of nine paragraphs, each repeat sentences long.
func playbackBody(repeat int) string {
	var body strings.Builder
	for i := range 9 {
		fmt.Fprintf(&body, "Paragraph %d as drafted: %s\n", i, strings.Repeat("the playback fixture observes one more thing about the draft under load. ", repeat))
	}
	return body.String()
}

// largePlayback brings the shipped capture over body and refs with long
// descriptions to its playback.
func largePlayback(t *testing.T, body string, bounded bool) (*mcp.ClientSession, string, mcpserver.ServeResult) {
	t.Helper()
	var entries []*model.Entry
	var refs []map[string]any
	var ids []string
	for n := range 8 {
		e := &model.Entry{
			ID: fmt.Sprintf("20260601-%06d-s-tac-p%02d", 100000+n, n), Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
			Summary: fmt.Sprintf("Entry %d of the playback fixture observes a gap.", n),
			Content: "The playback fixture observes a gap.", Topics: proctest.MustTopics("testing/fixture"), Confidence: "medium",
		}
		entries, ids = append(entries, e), append(ids, e.ID)
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
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &serve)
	capture := serve.Instance
	callRaw(t, cs, "gather", map[string]any{"session": session, "instance": capture, "ids": ids})
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{
		"body": body, "entryKind": "gap", "layer": "tactical", "refs": refs,
		"topics": []string{"testing/fixture"}, "confidence": "medium", "widenReport": "opened every fixture entry",
	}}, &serve)
	if serve.Step != "playback" {
		t.Fatalf("the draft should reach playback, got %q: %.300q", serve.Step, serve.Instructions)
	}
	return cs, session, serve
}

// requireDraftWholeAndFirst asserts a playback serve leads with the draft
// whole, never naming it among the lanes that wait.
func requireDraftWholeAndFirst(t *testing.T, serve mcpserver.ServeResult) {
	t.Helper()
	if !strings.HasPrefix(serve.Instructions, "Draft ") {
		t.Errorf("the playback should serve the draft first, got %.300q", serve.Instructions)
	}
	for _, want := range []string{"Paragraph 0 as drafted", "Paragraph 8 as drafted"} {
		if !strings.Contains(serve.Instructions, want) {
			t.Errorf("the playback should serve the draft whole, missing %q", want)
		}
	}
	if waiting := waitingLanes.FindStringSubmatch(serve.Instructions); waiting != nil && strings.Contains(waiting[1], "draft") {
		t.Errorf("the draft should never wait, got %q", waiting)
	}
}

// A playback serves the draft whole and first, within the bound or alone
// past it, and the confirm after it applies.
func TestAPlaybackServesItsDraftWholeAndFirst(t *testing.T) {
	for _, tc := range []struct {
		name      string
		repeat    int
		bounded   bool
		pastBound bool
	}{
		{name: "within the result bound", repeat: 8, bounded: true},
		{name: "a draft past the result bound", repeat: 40, pastBound: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, session, serve := largePlayback(t, playbackBody(tc.repeat), tc.bounded)
			requireDraftWholeAndFirst(t, serve)
			if tc.pastBound && model.ServedChars(serve.Instructions) <= model.DefaultResultBound.Serve() {
				t.Fatalf("the draft should run past the bound alone, the fixture too small to test: %d chars", model.ServedChars(serve.Instructions))
			}
			for calls := 0; waitingLanes.MatchString(serve.Instructions); calls++ {
				if calls == 10 {
					t.Fatalf("the waiting lanes should drain, still waiting: %.300q", serve.Instructions)
				}
				call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance}, &serve)
			}
			call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance, "report": map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "write it"}}, &serve)
			if serve.Step == "playback" {
				t.Fatalf("the confirm should apply, it served the playback again: %.500q", serve.Instructions)
			}
		})
	}
}
