package mcpapp_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
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

// The result ending a move lands on the session base short: the move's
// outcome, the base step named with its choices and the moves to start, none
// of the base step's text; resume_session serves that text in full
// (20261121-233256-d-tac-mm2).
func TestAMoveEndsWithAShortLanding(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	door := openSessionWhole(t, cs)
	const baseText = "Ending is part of the arc"
	if !strings.Contains(door.Instructions, baseText) {
		t.Fatalf("setup: the opening should carry the base step's text, got %.300q", door.Instructions)
	}

	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": door.Session, "canonical": "capture", "params": map[string]any{"anchor": fixtureGapID}}, &serve)
	var ended mcpserver.AbandonResult
	call(t, cs, "abandon", map[string]any{"session": door.Session, "instance": serve.Instance, "reason": "not needed"}, &ended)
	name, base := "abandon", ended.Base
	{
		if base == nil || base.Instance != door.Instance || base.Step != "junction" {
			t.Fatalf("%s should land on the base step, got %+v", name, base)
		}
		text := base.Instructions
		for _, want := range []string{"back at user-dialogue, step junction", "resume_session sends them in full", "Moves to start next: ", "capture"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s's landing should carry %q, got %q", name, want, text)
			}
		}
		if strings.Contains(text, baseText) {
			t.Errorf("%s's landing should not carry the base step's text, got %q", name, text)
		}
		if base.PendingChooser == nil || len(base.PendingChooser.Options) == 0 {
			t.Errorf("%s's landing should carry the base step's choices, got %+v", name, base.PendingChooser)
		}
	}

	var resumed mcpserver.ResumeSessionResult
	call(t, cs, "resume_session", map[string]any{"session": door.Session}, &resumed)
	for _, open := range resumed.Open {
		if open.Instance == door.Instance && !strings.Contains(open.Instructions, baseText) && !strings.Contains(open.Instructions, "lanes of these instructions wait") {
			t.Fatalf("resume_session should serve the base step in full, got %.300q", open.Instructions)
		}
	}
}

// A move ending on a call that answers lost lands on the base step's text in
// full: the answer emptied the held set (20261122-105026-d-tac-wl5).
// Framing stays once per change (TestCompactionAnswersLost).
func TestALostAnswerLandsOnTheBaseStepInFull(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	door := openSessionWhole(t, cs)

	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": door.Session, "canonical": "capture", "params": map[string]any{"anchor": fixtureGapID}}, &serve)
	var ended mcpserver.AbandonResult
	call(t, cs, "abandon", map[string]any{"session": door.Session, "instance": serve.Instance, "reason": "not needed", "answer": "lost"}, &ended)
	base := ended.Base
	if base == nil || base.Instance != door.Instance {
		t.Fatalf("abandon should land on the base step, got %+v", base)
	}
	if !strings.Contains(base.Instructions, "Ending is part of the arc") && !strings.Contains(base.Instructions, "lanes of these instructions wait") {
		t.Errorf("the landing should serve the base step's text, got %.300q", base.Instructions)
	}
	if strings.Contains(base.Instructions, "resume_session sends them in full") {
		t.Errorf("the landing should not point at a text it serves, got %.300q", base.Instructions)
	}
}

// A refused call answers no question, and its refusal says the questions the
// last response ended with still stand.
func TestARefusedCallSaysTheQuestionsStand(t *testing.T) {
	env := newTestServer(t, nil, "", "")
	cs := connect(t, env.srv)
	door := openSessionWhole(t, cs)

	refusal := callExpectError(t, cs, "abandon", map[string]any{"session": door.Session, "instance": "i_99", "reason": "not needed"})
	if !strings.Contains(refusal, "questions the last response ended with still stand") {
		t.Errorf("the refusal should say the questions still stand, got %q", refusal)
	}

	var serve mcpserver.ServeResult
	call(t, cs, "start_procedure", map[string]any{"session": door.Session, "canonical": "capture", "params": map[string]any{"anchor": fixtureGapID}}, &serve)
	events, err := readSessionLog(t, env.sessionsDir, door.Session)
	if err != nil {
		t.Fatal(err)
	}
	var last engine.Event
	for _, event := range events {
		if event.Event == engine.EventTexts {
			last = event
		}
	}
	if _, answered := last.Data["answered"]; !answered || last.Data["reset"] == true {
		t.Errorf("the answer the refused call carried should answer the next call, got %v", last.Data)
	}
}
