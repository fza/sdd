package mcpapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/engine"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

const restagedPlaybackBody = "A tactical gap in text the user has not seen yet.\n"

const stagedPlaybackProcedure = `---
type: decision
layer: process
kind: procedure
canonical: stagedplayback
participants:
    - Tester
confidence: medium
summary: A playback over a staged body.
state:
    body: {type: text, desc: the body}
    bodyHandle: {type: attachment-handle, optional: true, desc: staged file holding the body}
    bodyHash: {type: text, optional: true, hostSet: true, desc: set by sdd from bodyHandle}
steps:
    - id: assemble
      collect: [body, "bodyHandle?"]
      transitions:
          - when: hasBody
            to: playback
    - id: playback
      chooser: user
      serveDelta: [body, bodyHandle]
      options:
          - {choice: confirm, call: confirmPlayback, to: end(completed)}
---

A playback over a staged body.

## unit: assemble

Draft the body.

## unit: playback

Play back to the user: {{.body}}
`

// newPlaybackServer serves the fixture graph plus a procedure playing a
// staged body back.
func newPlaybackServer(t *testing.T, mutate ...func(*testServerOptions)) testEnv {
	t.Helper()
	graphDir := writeFixtureGraph(t)
	path := filepath.Join(graphDir, "2026", "07", "05-120000-d-prc-stp.md")
	if err := os.WriteFile(path, []byte(stagedPlaybackProcedure), 0o644); err != nil {
		t.Fatal(err)
	}
	return newTestServer(t, nil, graphDir, "", mutate...)
}

// stagePlaybackCapture brings a playback over a staged body to its chooser,
// then re-stages the body so the change waits to be played back.
func stagePlaybackCapture(t *testing.T, cs *mcp.ClientSession, session string) string {
	t.Helper()
	var staged mcpserver.StageAttachmentResult
	call(t, cs, "stage_attachment", map[string]any{"session": session, "files": []map[string]any{{"name": "draft.md", "content": "A tactical gap drafted in a staged file.\n"}}}, &staged)
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "stagedplayback"}, &serve)
	report := map[string]any{"bodyHandle": staged.StagedFiles()[0].Handle}
	call(t, cs, "next", map[string]any{"session": session, "instance": serve.Instance, "report": report}, &serve)
	if serve.Step != "playback" {
		t.Fatalf("capture must stand at playback, got %s", serve.Step)
	}
	call(t, cs, "stage_attachment", map[string]any{"session": session, "files": []map[string]any{{"name": "draft.md", "content": restagedPlaybackBody}}}, &staged)
	return serve.Instance
}

func confirmPlayback(t *testing.T, cs *mcp.ClientSession, session, instance string) *mcp.CallToolResult {
	t.Helper()
	return rawCall(t, cs, "next", map[string]any{"session": session, "instance": instance, "report": map[string]any{
		"chooser": "playback", "choice": "confirm", "userWords": "capture it",
	}})
}

// requirePlayedBackInstead asserts a confirm met the unserved change and
// served it rather than advancing.
func requirePlayedBackInstead(t *testing.T, res *mcp.CallToolResult) {
	t.Helper()
	var serve mcpserver.ServeResult
	decodeStructured(t, res, &serve)
	if serve.Step != "playback" || !strings.Contains(serve.Instructions, "not seen yet") {
		t.Fatalf("a confirm over a change no client received should play it back instead, got %s\n%s", serve.Step, serve.Instructions)
	}
}

func TestPlaybackFailingToRecordStaysUnserved(t *testing.T) {
	store := &sessionStoreProbe{}
	env := newPlaybackServer(t, func(opts *testServerOptions) {
		store.SessionStore = opts.Application.Sessions
		opts.Application.Sessions = store
	})
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session
	instance := stagePlaybackCapture(t, cs, session)

	store.FailNextEvent(engine.EventServed)
	if res := confirmPlayback(t, cs, session, instance); !res.IsError {
		t.Fatalf("a playback whose record fails should fail the call, got %s", contentText(res))
	}
	requirePlayedBackInstead(t, confirmPlayback(t, cs, session, instance))
	var serve mcpserver.ServeResult
	decodeStructured(t, confirmPlayback(t, cs, session, instance), &serve)
	if serve.Status != "completed" {
		t.Errorf("a confirm after a delivered playback should apply, got %s at %s", serve.Status, serve.Step)
	}
}

func TestResumeOmittingAPlaybackLeavesItUnserved(t *testing.T) {
	env := newPlaybackServer(t)
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session
	// Eight moves fill the resume cap; the capture started after them is past it.
	for range 8 {
		call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture", "params": map[string]any{"anchor": fixtureGapID}}, new(mcpserver.ServeResult))
	}
	instance := stagePlaybackCapture(t, cs, session)

	var resumed mcpserver.ResumeSessionResult
	call(t, cs, "resume_session", map[string]any{"session": session}, &resumed)
	for _, open := range resumed.Open {
		if open.Instance == instance {
			t.Fatal("the resume should omit the capture past the count cap")
		}
	}
	requirePlayedBackInstead(t, confirmPlayback(t, cs, session, instance))
}
