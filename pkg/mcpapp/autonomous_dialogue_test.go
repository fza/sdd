package mcpapp_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// autonomousAccess opens every session in the autonomous dialogue mode.
type autonomousAccess struct{ rootAccess }

func (autonomousAccess) ResolveDialogue(context.Context, sdd.Principal, sdd.ProjectID) (sdd.Dialogue, error) {
	return sdd.Dialogue{Mode: "auto", Source: "config.local.yaml"}, nil
}

func TestResumeAddressesTheSessionsMode(t *testing.T) {
	cases := []struct {
		name       string
		autonomous bool
		want       []string
		unwanted   []string
	}{
		{name: "guided", want: []string{"Brief the user", "present them to the\nuser"}},
		{name: "autonomous", autonomous: true, want: []string{"Continue through next", "finish or park"}, unwanted: []string{"Brief the user", "present them to the\nuser"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestServer(t, nil, "", "", func(opts *testServerOptions) {
				if tc.autonomous {
					opts.Application.Access = autonomousAccess{opts.Application.Access.(rootAccess)}
				}
			})
			cs := connect(t, env.srv)
			door := openSession(t, cs)
			var serve mcpserver.ServeResult
			call(t, cs, "start_procedure", map[string]any{"session": door.Session, "canonical": "capture"}, &serve)

			var resumed mcpserver.ResumeSessionResult
			call(t, cs, "resume_session", map[string]any{"session": door.Session}, &resumed)
			text := resumed.Instructions
			for _, open := range resumed.Open {
				text += "\n" + open.OpenThreads
			}
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("the resume should address the session's mode; missing %q in %q", want, text)
				}
			}
			for _, unwanted := range tc.unwanted {
				if strings.Contains(text, unwanted) {
					t.Errorf("an autonomous resume should not ask to brief a user; found %q", unwanted)
				}
			}
		})
	}
}

func TestAutonomousConcludeNamesWhatItLeavesBehind(t *testing.T) {
	env := newTestServer(t, nil, "", "", func(opts *testServerOptions) {
		opts.Application.Access = autonomousAccess{opts.Application.Access.(rootAccess)}
	})
	cs := connect(t, env.srv)
	shell := openSession(t, cs)

	var capture mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": shell.Session, "canonical": "capture"}, &capture)
	var serve mcpserver.ServeResult
	call(t, cs, "next", map[string]any{"session": shell.Session, "instance": shell.Instance, "report": map[string]any{
		"chooser": "junction", "choice": "conclude", "reason": "the work is done, the capture is not needed",
	}}, &serve)

	if serve.Status != "completed" {
		t.Fatalf("the agent should conclude an autonomous session, got %s at %q", serve.Status, serve.Step)
	}
	if !strings.Contains(serve.OpenThreads, capture.Instance) || !strings.Contains(serve.OpenThreads, "Name them in what you report") || strings.Contains(serve.OpenThreads, "Tell the user") {
		t.Errorf("an autonomous conclude should ask the agent to report what it leaves behind, got %q", serve.OpenThreads)
	}
}

// blockedProcedure is a move whose first step is an unattended user chooser,
// standing in for a capture blocked at reviseOrOverride.
const blockedProcedure = `---
type: decision
layer: process
kind: procedure
canonical: blocked
participants:
    - Tester
confidence: medium
summary: A move that waits at an unattended gate.
steps:
    - id: gate
      chooser: user
      unattended: {answer: revise, limit: 1}
      options:
          - {choice: revise, to: gate}
          - {choice: override, to: end(completed)}
---

## unit: gate

A blocked check.
`

// Dropping a move blocked at an unattended step is a person's call: the
// agent's abandon by session handle is refused while the session is
// autonomous, and the move stays open.
func TestAutonomousAbandonBySessionKeepsABlockedMove(t *testing.T) {
	graphDir := writeFixtureGraph(t)
	path := filepath.Join(graphDir, "2026", "07", "05-120000-d-prc-blk.md")
	if err := os.WriteFile(path, []byte(blockedProcedure), 0o644); err != nil {
		t.Fatal(err)
	}
	env := newTestServer(t, nil, graphDir, "", func(opts *testServerOptions) {
		opts.Application.Access = autonomousAccess{opts.Application.Access.(rootAccess)}
	})
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session
	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "blocked"}, &serve)
	if serve.Step != "gate" {
		t.Fatalf("precondition: the move should wait at its unattended gate, got %q", serve.Step)
	}
	refusal := callExpectError(t, cs, "abandon", map[string]any{"session": session, "reason": "drop it"})
	if !strings.Contains(refusal, "is not abandoned in an autonomous session") {
		t.Fatalf("the agent's abandon by session handle should be refused while a move waits at an unattended step, got %q", refusal)
	}
	var resumed mcpserver.ResumeSessionResult
	call(t, cs, "resume_session", map[string]any{"session": session}, &resumed)
	if len(resumed.Open) == 0 {
		t.Fatalf("the refused abandon should leave the move open, got %+v", resumed)
	}
}
