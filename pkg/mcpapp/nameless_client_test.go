package mcpapp_test

import (
	"strings"
	"testing"

	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// A client that sends no name keeps the session, the reads and abandon in
// auto mode; only a capture, which writes for its starting client, is refused.
func TestNamelessClient_InAutoModeIsRefusedOnlyACapture(t *testing.T) {
	env := newTestServer(t, nil, "", "", func(opts *testServerOptions) {
		opts.Application.Access = autonomousAccess{opts.Application.Access.(rootAccess)}
	})
	cs := connectNamedAs(t, env.srv, newAgentMemory(), "")
	var door mcpserver.ServeResult
	call(t, cs, "start_session", map[string]any{}, &door)

	reads := []struct {
		tool string
		args map[string]any
	}{
		{"show", map[string]any{"session": door.Session, "ids": []string{fixtureGapID}}},
		{"search", map[string]any{"session": door.Session, "terms": []string{"oscillation"}}},
		{"view", map[string]any{"session": door.Session, "layout": "active:as-counts"}},
	}
	for _, read := range reads {
		if text, isError := callOutcome(t, cs, read.tool, read.args); isError {
			t.Errorf("%s should serve a nameless client, got %s", read.tool, text)
		}
	}

	refusal := callExpectError(t, cs, "start_procedure", map[string]any{"session": door.Session, "canonical": "capture"})
	if !strings.Contains(refusal, "send a name") {
		t.Errorf("an unattended capture from a nameless client should be refused telling it to send a name, got %s", refusal)
	}

	if text, isError := callOutcome(t, cs, "abandon", map[string]any{"session": door.Session, "reason": "done"}); isError {
		t.Errorf("abandon should serve a nameless client, got %s", text)
	}
}
