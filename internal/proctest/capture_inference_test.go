package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/test"
)

// A capture never supplies the why or a directive's intent: assemble takes
// them from the user's own words, and playback marks every other sentence the
// user did not say as the agent's inference.
func TestCapture_NeverSuppliesTheWhyOrTheIntent(t *testing.T) {
	_, session := newCaptureWorld(t, "capture-no-supplied-why")
	serve := session.Start(t, "capture", nil)
	proctest.RequireStep(t, serve, "assemble")
	assemble := test.Untagged(serve.Instructions)
	for _, want := range []string{
		"The dialogue holds none? Ask the user, end your turn, and draft after the answer. Never supply them.",
		"A decision's reason states what the user said in `reasonWords`; never supply one.",
		"It follows what the user said in `intentWords`; never supply one.",
	} {
		if !strings.Contains(assemble, want) {
			t.Errorf("assemble should carry %q:\n%s", want, assemble)
		}
	}
	serve = session.Report(t, serve.Instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	playback := test.Untagged(serve.Instructions)
	for _, want := range []string{
		`Mark to the user, as your inference ("my inference: …"), every sentence of elaboration in the draft that the user did not say.`,
		"A decision's reason and a directive's intent are never inference: show the user their quoted words from the draft block.",
	} {
		if !strings.Contains(playback, want) {
			t.Errorf("playback should carry %q:\n%s", want, playback)
		}
	}
}

// An autonomous capture has no user to ask, so its assemble serves no rule
// to ask for the user's words.
func TestCapture_AutonomousAssembleAsksNoUserWords(t *testing.T) {
	_, session := newCaptureWorld(t, "capture-autonomous-no-ask", proctest.WithAutonomousDialogue())
	serve := session.Start(t, "capture", nil)
	proctest.RequireStep(t, serve, "assemble")
	if assemble := test.Untagged(serve.Instructions); strings.Contains(assemble, "reasonWords") || strings.Contains(assemble, "intentWords") {
		t.Errorf("an autonomous assemble should not ask for the user's words:\n%s", assemble)
	}
}
