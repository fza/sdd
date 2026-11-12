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
