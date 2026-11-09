package mcpapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

const relayDispatcherProcedure = `---
type: decision
kind: procedure
layer: process
canonical: relay
confidence: high
summary: A move dispatching a one-shot delegate for the transport tests.
state:
    goal: {type: text, desc: what to brief}
    briefing: {type: text, desc: the delegate's result}
steps:
    - id: ask
      chooser: agent
      options:
          - {choice: delegate, collect: [goal], dispatch: {procedure: relaytask, seed: {topic: goal}, receive: {briefing: body}}, to: wait}
    - id: wait
      await: [briefing]
      transitions:
          - otherwise: end(completed)
---

Relays a brief.
`

const relayTaskProcedure = `---
type: decision
kind: procedure
layer: process
canonical: relaytask
class: task
confidence: high
summary: A one-shot delegate task for the transport tests.
state:
    topic: {type: text, desc: what to brief}
    body: {type: text, desc: the brief}
steps:
    - id: brief
      collect: [body]
      deliver: [body]
      transitions:
          - when: hasBody
            to: end(completed)
---

Briefs.
`

func TestDelegateDispatchOverTheTransport(t *testing.T) {
	graphDir := writeFixtureGraph(t)
	for rel, content := range map[string]string{
		"2026/07/05-120000-d-prc-rly.md": relayDispatcherProcedure,
		"2026/07/05-120100-d-prc-rlt.md": relayTaskProcedure,
	} {
		path := filepath.Join(graphDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env := newTestServer(t, nil, graphDir, "")
	client := connect(t, env.srv)
	door := openSession(t, client)

	var move mcpserver.ServeResult
	call(t, client, "start_procedure", map[string]any{"session": door.Session, "canonical": "relay"}, &move)
	call(t, client, "next", map[string]any{"session": door.Session, "instance": move.Instance,
		"report": map[string]any{"chooser": "ask", "choice": "delegate", "fields": map[string]any{"goal": "the open gaps"}}}, &move)

	var forked mcpserver.ServeResult
	call(t, client, "start_procedure", map[string]any{"session": door.Session, "canonical": "relaytask", "parent": move.Instance}, &forked)
	if forked.Session != door.Session || forked.Instance != move.Instance || len(forked.Dispatches) != 1 {
		t.Fatalf("the dispatch should serve the waiting move with one handoff, got %+v", forked)
	}
	dispatch := forked.Dispatches[0]
	if dispatch.Session == door.Session || strings.Contains(dispatch.Prompt, door.Session) {
		t.Fatalf("the handoff carries the child handle alone, got %+v", dispatch)
	}

	var waiting mcpserver.ServeResult
	call(t, client, "next", map[string]any{"session": door.Session, "instance": move.Instance}, &waiting)
	if waiting.Step != "wait" || !strings.Contains(waiting.Instructions, "waiting for the delegate to deliver briefing") {
		t.Fatalf("a report-less next re-serves the waiting move, got %s %q", waiting.Step, waiting.Instructions)
	}

	delegate := connect(t, env.srv)
	var resumed mcpserver.ResumeSessionResult
	call(t, delegate, "resume_session", map[string]any{"session": string(dispatch.Session)}, &resumed)
	if len(resumed.Open) != 1 || resumed.Open[0].Procedure != "relaytask" {
		t.Fatalf("the delegate resumes its child session by handle, got %+v", resumed.Open)
	}
	var done mcpserver.ServeResult
	call(t, delegate, "next", map[string]any{"session": string(dispatch.Session), "instance": dispatch.Instance, "report": map[string]any{"body": "two gaps stay open"}}, &done)
	if done.Status != "completed" || !strings.Contains(done.Instructions, "Stop here") {
		t.Fatalf("the final delivery ends the delegate, got %s %q", done.Status, done.Instructions)
	}
	if message := callExpectError(t, delegate, "next", map[string]any{"session": string(dispatch.Session), "instance": dispatch.Instance}); !strings.Contains(message, "session_ended") {
		t.Fatalf("a finished delegate session takes no more calls, got %q", message)
	}

	var picked mcpserver.ServeResult
	call(t, client, "next", map[string]any{"session": door.Session, "instance": move.Instance}, &picked)
	if picked.Status != "completed" {
		t.Fatalf("the pickup lands the delivered brief, got %s at %s", picked.Status, picked.Step)
	}
}
