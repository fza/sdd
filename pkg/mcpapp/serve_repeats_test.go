package mcpapp_test

import (
	"strings"
	"testing"

	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// recentMovement is the framing block every fixture session carries, which
// no call in these tests changes.
const recentMovement = "Recent graph movement"

// A framing block goes to each caller once and again only when it changes,
// with no proof asked; a caller the session has not served, marked by caller,
// gets every block (20261121-233256-d-tac-mm2).
func TestFramingServesEachCallerOncePerChange(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	door := openSessionWhole(t, cs)
	if !strings.Contains(door.Framing, recentMovement) {
		t.Fatalf("the opening should carry the whole framing, got %q", door.Framing)
	}

	var move mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": door.Session, "canonical": "capture"}, &move)
	if strings.Contains(move.Framing, recentMovement) {
		t.Fatalf("the first caller holds the unchanged blocks and should not get them again, got %q", move.Framing)
	}

	var sub mcpserver.ServeResult
	call(t, cs, "next", map[string]any{"session": door.Session, "instance": move.Instance, "caller": "verifier"}, &sub)
	if !strings.Contains(sub.Framing, recentMovement) {
		t.Fatalf("a caller the session has not served should get every block, got %q", sub.Framing)
	}
	call(t, cs, "next", map[string]any{"session": door.Session, "instance": move.Instance, "caller": "verifier"}, &sub)
	if strings.Contains(sub.Framing, recentMovement) {
		t.Fatalf("the second caller should get an unchanged block once, got %q", sub.Framing)
	}
	call(t, cs, "next", map[string]any{"session": door.Session, "instance": move.Instance}, &move)
	if strings.Contains(move.Framing, recentMovement) {
		t.Fatalf("serving the second caller should leave the first caller's record alone, got %q", move.Framing)
	}

	var resumed mcpserver.ResumeSessionResult
	call(t, cs, "resume_session", map[string]any{"session": door.Session}, &resumed)
	framing := resumed.Framing
	if strings.Contains(resumed.Instructions, "framing blocks wait") {
		// The blocks past the result bound serve on the shell's next call,
		// though the caller was sent them before.
		var rest mcpserver.ServeResult
		call(t, cs, "next", map[string]any{"session": door.Session, "instance": door.Instance}, &rest)
		framing += rest.Framing
	}
	if !strings.Contains(framing, recentMovement) {
		t.Fatalf("resume_session should carry every framing block, got %q", framing)
	}
}
