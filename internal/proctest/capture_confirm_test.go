package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/test"
)

// Playback confirms only on the user's own answer to the draft shown in
// full, never in the turn that first shows it, and relays the user's words,
// never the agent's.
func TestCapture_PlaybackConfirmsOnlyOnTheUsersOwnAnswer(t *testing.T) {
	_, session := newCaptureWorld(t, "capture-confirm-own-answer")
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	want := "Confirm only on the user's own answer to the draft you showed them in full, never in the turn you first show it: show it, end your turn and wait. `userWords` is the user's message verbatim, never words of yours."
	if text := test.Untagged(serve.Instructions); !strings.Contains(text, want) {
		t.Fatalf("playback should carry %q:\n%s", want, text)
	}
}
