package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/test"
)

// The verifier marks a finding on facts only the user knows as the user's,
// never as a missing reference the drafter could add.
func TestCaptureDeep_TheVerifierGivesFactsOnlyTheUserKnowsToTheUser(t *testing.T) {
	world, session := newDeepWorld(t, "deep-owner-facts", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	want := "So is one that turns on facts only the user knows, such as whether incidents were ever recorded: never raise it as `missing-ref` for the drafter to fix."
	for range 20 {
		serve := x.delegate.Answer(t, x.child, "read", "more", nil, "")
		proctest.RequireStep(t, serve, "read")
		text := test.Untagged(serve.Instructions)
		if strings.Contains(text, "Every page is served.") {
			if !strings.Contains(text, want) {
				t.Fatalf("the verifier's judge guidance should carry %q:\n%s", want, text)
			}
			return
		}
	}
	t.Fatal("the reading should end within 20 pages")
}

// An open point reaches the user with the draft's position and the verifier's,
// and the agent puts the choice in plain words, never in sdd's names for it.
func TestCaptureDeep_AnOpenPointGoesToTheUserWithBothPositions(t *testing.T) {
	world, session := newDeepWorld(t, "deep-owner-positions", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{map[string]any{
		"verdict": "whether this gap is in scope is the owner's call", "axis": "aspiration", "severity": "medium", "owner": true,
		"pointers": []any{map[string]any{"field": "body", "quote": "observes something"}},
	}})
	x.pickUp(t)
	session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	serve := session.Answer(t, x.instance, "deepProposal", "accept", nil, "")
	proctest.RequireStep(t, serve, "deepUser")
	text := test.Untagged(serve.Instructions)
	for _, want := range []string{
		"- f1, only the user can settle it\n  draft: body says \"observes something\"\n  verifier: whether this gap is in scope is the owner's call\n",
		"ask in plain words whether the draft stands as it is, what to change, or whether to drop the capture.",
		"Never show the user finding labels, option ids or step names.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("deepUser should carry %q:\n%s", want, text)
		}
	}
}
