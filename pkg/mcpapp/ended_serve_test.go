package mcpapp_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
	"github.com/networkteam/sdd/test"
)

// A capture that ends after a deep exchange over a large reading serves its
// completion within a serve's share of the result bound to an agent that
// lists no proofs: the entry it wrote, the working state it leaves out named,
// and the shell's lanes that wait serving on the shell's next call. The
// conclude serve stays within the bound too.
func TestAnEndedMoveServesWithinTheResultBound(t *testing.T) {
	var entries []*model.Entry
	var ids []string
	for n := range 8 {
		var body strings.Builder
		for i := range 40 {
			fmt.Fprintf(&body, "Paragraph %d of entry %d observes how the reading behaves under load, with enough words to fill a line.\n\n", i, n)
		}
		e := &model.Entry{
			ID: fmt.Sprintf("20260601-%06d-s-tac-e%02d", 100000+n, n), Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
			Summary: fmt.Sprintf("Entry %d of the ended fixture observes the reading under load.", n),
			Content: body.String(), Topics: proctest.MustTopics("testing/fixture"), Confidence: "medium",
		}
		entries, ids = append(entries, e), append(ids, e.ID)
	}
	entries = append(entries, proctest.RealisticGraph(proctest.DefaultBase(), youngProjectShape)...)
	world := proctest.NewWorld(t, proctest.WithEntries(entries...), proctest.WithVerifier(sdd.VerifierPolicy{Mode: "deep"}, 200000, 4096))
	srv, err := mcpserver.New(mcpserver.Options{Application: world.App, SearchSyncMode: sdd.SearchSyncAll, LocalIdentity: world.Identity, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	cs, bound := boundedConnect(t, srv, false)
	defer func() { t.Logf("largest results: %v", bound.largest) }()
	session := openSession(t, cs).Session
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &serve)
	capture := serve.Instance
	callRaw(t, cs, "gather", map[string]any{"session": session, "instance": capture, "ids": ids})
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{
		"body": "A tactical gap: the ended fixture observes the reading under load.", "entryKind": "gap", "layer": "tactical",
		"refs":   []map[string]any{{"id": ids[0], "kind": "related", "desc": "the entry this gap sits beside"}},
		"topics": []string{"testing/fixture"}, "confidence": "medium", "widenReport": "opened every fixture entry",
	}}, &serve)
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "write it"}}, &serve)
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "deepDispatch", "choice": "delegate"}}, &serve)
	if len(serve.Dispatches) != 1 {
		t.Fatalf("the dispatching answer should owe one dispatch, got %+v", serve.Dispatches)
	}
	child, verify := serve.Dispatches[0].Session, serve.Dispatches[0].Instance
	var page mcpserver.ServeResult
	for n := 0; !strings.Contains(test.Untagged(page.Instructions), "Every page is served."); n++ {
		if n > 40 {
			t.Fatal("the reading should end within 40 pages")
		}
		call(t, cs, "next", map[string]any{"session": child, "instance": verify, "report": map[string]any{"chooser": "read", "choice": "more"}}, &page)
	}
	call(t, cs, "next", map[string]any{"session": child, "instance": verify, "report": map[string]any{"chooser": "read", "choice": "judge", "fields": map[string]any{"findings": []any{}}}}, &page)
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "deepLaunch", "choice": "started"}}, &serve)
	if serve.Step != "verifySummary" {
		t.Fatalf("the clean verdict should reach the summary check, got %q: %.300q", serve.Step, serve.Instructions)
	}
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "verifySummary", "choice": "faithful", "fields": map[string]any{"fidelityNote": "matches"}}}, &serve)
	if serve.Status != "completed" || serve.Produced["entryId"] == "" || serve.Base == nil {
		t.Fatalf("the completion should name the entry it wrote and the shell it lands in, got %s %v", serve.Status, serve.Produced)
	}
	if size := jsonSize(t, serve); size > model.MaxServeChars {
		t.Fatalf("the completion should cost at most %d characters, got %d", model.MaxServeChars, size)
	}
	if _, ok := serve.Produced["deepPages"]; ok || !strings.Contains(serve.Instructions, "deepPages") {
		t.Fatalf("the completion should leave the exchange's reading out and name it, got %q", serve.Instructions)
	}
	shell := serve.Base.Instance
	if strings.Contains(serve.Base.Instructions, "lanes of these instructions wait") {
		var junction mcpserver.ServeResult
		call(t, cs, "next", map[string]any{"session": session, "instance": shell}, &junction)
		if text := test.Untagged(junction.Instructions); strings.Contains(text, "lanes of these instructions wait") || !strings.Contains(text, "Ending is part of the arc") {
			t.Fatalf("the shell's waiting lanes should serve on the next call, got %.300q", text)
		}
	}
	call(t, cs, "next", map[string]any{"session": session, "instance": shell, "report": map[string]any{"chooser": "junction", "choice": "conclude", "userWords": "that's all"}}, &serve)
	if serve.Status != "completed" {
		t.Fatalf("the session should conclude, got %s", serve.Status)
	}
}

// A move abandoned or parked lands on the shell's junction within a serve's
// share of the result bound for an agent that lists no proofs: the shell's
// last lanes wait and serve on its next call.
func TestALandingOnTheShellServesWithinTheResultBound(t *testing.T) {
	for _, tool := range []string{"abandon", "park"} {
		t.Run(tool, func(t *testing.T) {
			cs, _ := boundedConnect(t, newTestServer(t, nil, writeYoungProjectGraph(t), "").srv, false)
			session := openSession(t, cs).Session
			var serve mcpserver.ServeResult
			call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &serve)
			landed := callRaw(t, cs, tool, map[string]any{"session": session, "instance": serve.Instance})
			if size := jsonSize(t, landed); size > model.MaxServeChars {
				t.Fatalf("the %s landing should cost at most %d characters, got %d", tool, model.MaxServeChars, size)
			}
			base, _ := landed["base_junction"].(map[string]any)
			instructions, _ := base["instructions"].(string)
			shell, _ := base["instance"].(string)
			if !strings.Contains(instructions, "lanes of these instructions wait") {
				return
			}
			var junction mcpserver.ServeResult
			call(t, cs, "next", map[string]any{"session": session, "instance": shell}, &junction)
			if text := test.Untagged(junction.Instructions); strings.Contains(text, "lanes of these instructions wait") || !strings.Contains(text, "Ending is part of the arc") {
				t.Fatalf("the shell's waiting lanes should serve on the next call, got %.300q", text)
			}
		})
	}
}
