package mcpapp_test

import (
	"context"
	"strings"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// autonomousAccess opens every session in the autonomous dialogue mode
// (20261029-150854-d-tac-b2f).
type autonomousAccess struct{ rootAccess }

func (autonomousAccess) ResolveDialogue(context.Context, sdd.Principal, sdd.ProjectID) (sdd.Dialogue, error) {
	return sdd.Dialogue{Mode: "autonomous", Source: "config.local.yaml"}, nil
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
