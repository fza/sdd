package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/test"
)

// A capture never supplies the why or a directive's intent: assemble has the
// agent ask the user when the dialogue carries none, and playback marks every
// sentence of reasoning the user did not say as the agent's inference.
func TestCapture_NeverSuppliesTheWhyOrTheIntent(t *testing.T) {
	_, session := newCaptureWorld(t, "capture-no-supplied-why")
	serve := session.Start(t, "capture", nil)
	proctest.RequireStep(t, serve, "assemble")
	assemble := test.Untagged(serve.Instructions)
	for _, want := range []string{
		"Dialogue carries no reason for the decision? Ask the user for it before drafting; never supply one.",
		"Dialogue states no intent for the directive? Ask the user before drafting; never supply one.",
	} {
		if !strings.Contains(assemble, want) {
			t.Errorf("assemble should carry %q:\n%s", want, assemble)
		}
	}
	instance := serve.Instance
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	want := `Mark to the user, as your inference ("my inference: …"), every sentence of reasoning in the draft that the user did not say. Go sentence by sentence and mark each one, in every revised draft too.`
	if playback := test.Untagged(serve.Instructions); !strings.Contains(playback, want) {
		t.Errorf("playback should carry %q:\n%s", want, playback)
	}
	serve = session.Answer(t, instance, "playback", "adjust", map[string]any{"body": "A tactical gap: the fixture observes one thing."}, "say one thing")
	proctest.RequireStep(t, serve, "playback")
	if playback := test.Untagged(serve.Instructions); !strings.Contains(playback, want) {
		t.Errorf("the playback of a revised draft should carry %q:\n%s", want, playback)
	}
}
