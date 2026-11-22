package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	test "github.com/networkteam/sdd/internal/testsupport"
)

// A capture never passes its own words off as the user's: assemble quotes
// only words the user gave, and playback marks every sentence the user did not
// say, a proposed reason included, as the agent's inference.
func TestCapture_NeverPassesItsWordsOffAsTheUsers(t *testing.T) {
	_, session := newCaptureWorld(t, "capture-no-supplied-why")
	serve := session.Start(t, "capture", nil)
	proctest.RequireStep(t, serve, "assemble")
	assemble := test.Untagged(serve.Instructions)
	for _, want := range []string{
		"Never write words they did not give.",
		"Words the user gives for it go to `intentWords`; never write ones they did not.",
	} {
		if !strings.Contains(assemble, want) {
			t.Errorf("assemble should carry %q:\n%s", want, assemble)
		}
	}
	instance := serve.Instance
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	wants := []string{
		`Mark to the user, as your inference ("my inference: …"), every sentence of elaboration in the draft that the user did not say. Go sentence by sentence and mark each one, in every revised draft too.`,
		"Show a decision's reason and a directive's intent with the rest: the user's quoted words from the draft block as theirs, a proposal of yours as your inference, the intent in the user's terms; never name the posture.",
	}
	playback := test.Untagged(serve.Instructions)
	for _, want := range wants {
		if !strings.Contains(playback, want) {
			t.Errorf("playback should carry %q:\n%s", want, playback)
		}
	}
	serve = session.Answer(t, instance, "playback", "adjust", map[string]any{"body": "A tactical gap: the fixture observes one thing."}, "say one thing")
	proctest.RequireStep(t, serve, "playback")
	playback = test.Untagged(serve.Instructions)
	for _, want := range wants {
		if !strings.Contains(playback, want) {
			t.Errorf("the playback of a revised draft should carry %q:\n%s", want, playback)
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
