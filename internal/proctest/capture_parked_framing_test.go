package proctest_test

import (
	"strings"
	"testing"
)

// A person's next session names the captures an autonomous session parked,
// so they learn of them.
func TestUnattended_AGuidedOpeningNamesParkedCaptures(t *testing.T) {
	world, session, instance := blockedAutonomousCapture(t, "unattended-framing")
	parkAfterSecondBlock(t, session, instance)

	world.SetDialogue("guided")
	opened := world.Open(t, "person")
	blocks, err := opened.WF.FramingBlocks(t.Context(), world.Identity)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for _, block := range blocks {
		if block.Name == "parked" {
			text = block.Text
		}
	}
	for _, want := range []string{`"A tactical gap: the fixture observes something."`, "capture parked at reviseOrOverride since", "blocked again", "Resume: resume_session " + string(session.ID)} {
		if !strings.Contains(text, want) {
			t.Errorf("a guided opening should name the parked capture, missing %q in %q", want, text)
		}
	}
}

// An assisted session's opening names the drafts other sessions left at
// their approval gate, so the person can approve, batch or drop them; a move
// parked for a guided person stays out of it.
func TestAssisted_TheOpeningNamesDraftsWaitingForApproval(t *testing.T) {
	world, blocked, instance := blockedAutonomousCapture(t, "assisted-framing")
	parkAfterSecondBlock(t, blocked, instance)
	world.LLM.PreflightFindings = nil
	world.SetDialogue("assisted")
	session := world.Open(t, "claude-code")
	session.LogRead(t, "show", []string{captureRefID}, nil)
	sealedCapture(t, session, captureDraft())

	opened := world.Open(t, "claude-code")
	blocks, err := opened.WF.FramingBlocks(t.Context(), world.Identity)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for _, block := range blocks {
		if block.Name == "parked" {
			text = block.Text
		}
	}
	for _, want := range []string{"approve", "capture parked at playback since", "Resume: resume_session " + string(session.ID)} {
		if !strings.Contains(text, want) {
			t.Errorf("an assisted opening should name the draft waiting for approval, missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, "reviseOrOverride") {
		t.Errorf("an assisted opening should leave out moves parked for a guided person, got %q", text)
	}
}
