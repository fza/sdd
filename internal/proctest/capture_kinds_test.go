// Capture under per-kind verify modes (verify.kinds): the draft's kind picks
// the mode, and a kind checked looser than another gets the kind-fit check,
// since the drafter picks the kind.
package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/llm"
)

func TestCaptureKinds_ALooserKindGetsTheKindFitCheck(t *testing.T) {
	policy := sdd.VerifierPolicy{Mode: "prose", Kinds: map[string]string{"gap": "grounding"}, StricterKinds: map[string][]string{"gap": {"fact", "directive"}}}
	world, session := newCaptureWorld(t, "kinds-fit", proctest.WithGroundingEnforced(), proctest.WithVerifier(policy, 200000, 4096))
	_, serve := confirmedLightCapture(t, session)
	proctest.RequireStep(t, serve, "verifySummary")
	prompt := world.LLM.Prompts(llm.PurposePreflight)[0].UserPrompt
	if strings.Contains(prompt, "## Reading") {
		t.Error("a gap checked at grounding should get no prose verifier over its reading")
	}
	for _, want := range []string{"## Kind fit", "`gap` (the chosen kind)", "- `fact`:", "- `directive`:", "category `kind-fit`"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the kind-fit check should carry %q:\n%s", want, prompt)
		}
	}
}

func TestCaptureKinds_AKindAtTheModeGetsNoKindFitCheck(t *testing.T) {
	world, session := newLightWorld(t, "kinds-no-fit")
	_, serve := confirmedLightCapture(t, session)
	proctest.RequireStep(t, serve, "verifySummary")
	if prompt := world.LLM.Prompts(llm.PurposePreflight)[0].UserPrompt; strings.Contains(prompt, "## Kind fit") {
		t.Error("a kind checked at verify.mode needs no kind-fit check")
	}
}

func TestCaptureKinds_AKindBelowDeepSkipsTheDeepExchange(t *testing.T) {
	policy := sdd.VerifierPolicy{Mode: "deep", Kinds: map[string]string{"gap": "prose"}, StricterKinds: map[string][]string{"gap": {"directive"}}}
	world, session := newCaptureWorld(t, "kinds-below-deep", proctest.WithGroundingEnforced(), proctest.WithVerifier(policy, 200000, 4096))
	_, serve := confirmedLightCapture(t, session)
	proctest.RequireStep(t, serve, "verifySummary")
	prompt := world.LLM.Prompts(llm.PurposePreflight)[0].UserPrompt
	if !strings.Contains(prompt, "## Reading") || !strings.Contains(prompt, "## Kind fit") {
		t.Fatalf("a gap checked at prose under deep should get the prose verifier with the kind-fit check:\n%s", prompt)
	}
}

func TestCaptureKinds_AnOffKindOwesNoGroundingReading(t *testing.T) {
	policy := sdd.VerifierPolicy{Mode: "prose", Kinds: map[string]string{"gap": "off"}}
	_, session := newCaptureWorld(t, "kinds-off", proctest.WithGroundingEnforced(), proctest.WithVerifier(policy, 200000, 4096))
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
}
