package mcpapp_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// A staging call that re-advances a capture to its live verifier answers the
// handoff that continues the verifier, as a next call delivering the
// revision does.
func TestStageAttachmentAnswersTheDispatchItOwes(t *testing.T) {
	actor := &model.Entry{
		ID: "20260101-080000-s-prc-cli", Type: model.TypeSignal, Kind: model.KindActor, Layer: model.LayerProcess,
		Canonical: "Agent", Aliases: []string{"test-client"}, Content: "The agent behind the fixture's MCP client.",
	}
	world := proctest.NewWorld(t, proctest.WithEntries(actor), proctest.WithAssistedDialogue(), proctest.WithVerifier(sdd.VerifierPolicy{Mode: "deep"}, 200000, 4096))
	srv, err := mcpserver.New(mcpserver.Options{Application: world.App, SearchSyncMode: sdd.SearchSyncAll, LocalIdentity: world.Identity, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	cs := connect(t, srv)
	session := openSession(t, cs).Session
	call(t, cs, "stage_attachment", map[string]any{"session": session, "files": []any{map[string]any{
		"name": "draft.md", "content": "---\nentryKind: gap\nlayer: tac\n---\nA tactical gap: the fixture observes something.\n",
	}}}, &mcpserver.StageAttachmentResult{})
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &serve)
	capture := serve.Instance
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{
		"bodyHandle": "draft.md", "entryKind": "gap", "layer": "tactical", "confidence": "medium", "widenReport": "searched the fixture graph",
	}}, &serve)
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "deepDispatch", "choice": "delegate"}}, &serve)
	if len(serve.Dispatches) != 1 {
		t.Fatalf("the dispatching answer should owe one dispatch, got %q: %+v", serve.Step, serve.Dispatches)
	}
	child, verify := serve.Dispatches[0].Session, serve.Dispatches[0].Instance
	var page mcpserver.ServeResult
	for n := 0; !strings.Contains(page.Instructions, "Every page is served."); n++ {
		if n > 40 {
			t.Fatal("the reading should end within 40 pages")
		}
		call(t, cs, "next", map[string]any{"session": child, "instance": verify, "report": map[string]any{"chooser": "read", "choice": "more"}}, &page)
	}
	call(t, cs, "next", map[string]any{"session": child, "instance": verify, "report": map[string]any{"chooser": "read", "choice": "judge", "fields": map[string]any{"findings": []any{}}}}, &page)
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "deepLaunch", "choice": "started"}}, &serve)
	call(t, cs, "next", map[string]any{"session": session, "instance": capture, "report": map[string]any{"chooser": "playback", "choice": "adjust", "userWords": "say it happens twice"}}, &serve)
	if serve.Step != "assemble" {
		t.Fatalf("the person's adjust should send the draft back to assemble, got %q: %.300q", serve.Step, serve.Instructions)
	}

	var staged mcpserver.StageAttachmentResult
	call(t, cs, "stage_attachment", map[string]any{"session": session, "files": []any{map[string]any{
		"name": "draft.md", "patches": []any{map[string]any{"old": "observes something.", "new": "observes something twice."}},
	}}}, &staged)
	dispatches := staged.StageDispatches()
	if len(dispatches) != 1 || dispatches[0].Session != child || dispatches[0].ContinuePrompt == "" || !strings.Contains(dispatches[0].Instructions, "Continue the same delegate") {
		t.Fatalf("the staging should owe the handoff continuing the verifier, got %+v", dispatches)
	}
	call(t, cs, "next", map[string]any{"session": session, "instance": capture}, &serve)
	if serve.Step != "deepWait" || len(serve.Dispatches) != 0 {
		t.Fatalf("the waiting capture's next call should owe the harness's verifier nothing more, got %q with %+v", serve.Step, serve.Dispatches)
	}
}
