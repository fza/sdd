// Capture with the prose draft verifier (20261109-004729-d-cpt-xs5, slice 1):
// pre-flight runs over the grounding reading, adds the graph-fit axes, blocks
// on high findings as before and serves its one verdict otherwise.
package proctest_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/llm"
)

func newLightWorld(t *testing.T, connID string) (*proctest.World, *proctest.Session) {
	t.Helper()
	return newCaptureWorld(t, connID, proctest.WithGroundingEnforced(),
		proctest.WithVerifier(sdd.VerifierPolicy{Mode: "prose"}, 200000, 4096))
}

// confirmedLightCapture gathers, opens the unread fixture signal in full and
// sets it aside, reports the draft and confirms the playback.
func confirmedLightCapture(t *testing.T, session *proctest.Session) (string, *sdd.WorkflowServe) {
	t.Helper()
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance, captureRef2ID)
	draft := captureDraft()
	draft["verdicts"] = []any{map[string]any{"ids": []any{captureRef2ID}, "code": model.VerdictCovered}}
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
	return instance, session.Answer(t, instance, "playback", "confirm", nil, "write it")
}

func TestCaptureVerifier_LightReadsTheGroundingReading(t *testing.T) {
	world, session := newLightWorld(t, "light-reading")
	_, serve := confirmedLightCapture(t, session)
	proctest.RequireStep(t, serve, "verifySummary")
	prompts := world.LLM.Prompts(llm.PurposePreflight)
	if len(prompts) != 1 {
		t.Fatalf("one light call should judge the draft, got %d", len(prompts))
	}
	prompt := prompts[0].UserPrompt
	for _, want := range []string{"## Reading", "ID: " + captureRef2ID, "Role in the reading: set aside (code: " + model.VerdictCovered + ")", "`duplicates`", "`dismissal`"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the light pre-flight should carry %q", want)
		}
	}
}

func TestCaptureVerifier_OffKeepsPreflightOverTheRefs(t *testing.T) {
	world, session := newCaptureWorld(t, "light-off", proctest.WithGroundingEnforced())
	_, serve := confirmedLightCapture(t, session)
	proctest.RequireStep(t, serve, "verifySummary")
	prompt := world.LLM.Prompts(llm.PurposePreflight)[0].UserPrompt
	if strings.Contains(prompt, "## Reading") || strings.Contains(prompt, captureRef2ID) {
		t.Fatal("with the verifier off, pre-flight judges the draft's refs alone")
	}
}

func TestCaptureVerifier_LightBlocksOnHighAndRerunsAfterRevise(t *testing.T) {
	world, session := newLightWorld(t, "light-high")
	world.LLM.PreflightFindings = []proctest.PreflightFinding{{Severity: "high", Category: "duplicates", Observation: "the draft repeats a live entry"}}
	instance, serve := confirmedLightCapture(t, session)
	proctest.RequireStep(t, serve, "reviseOrOverride")
	if !strings.Contains(serve.Instructions, "duplicates: the draft repeats a live entry") {
		t.Fatalf("the blocking finding should be served:\n%s", serve.Instructions)
	}

	world.LLM.PreflightFindings = nil
	serve = session.Answer(t, instance, "reviseOrOverride", "revise", map[string]any{"body": "A tactical gap: the fixture observes something new."}, "revise it")
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, instance, "playback", "confirm", nil, "now write it")
	proctest.RequireStep(t, serve, "verifySummary")
	if got := world.LLM.Calls(llm.PurposePreflight); got != 2 {
		t.Fatalf("a revised draft goes through pre-flight again, ran %d times", got)
	}
}

func TestCaptureVerifier_LightServesOneVerdictThenWrites(t *testing.T) {
	world, session := newLightWorld(t, "light-verdict")
	world.LLM.PreflightFindings = []proctest.PreflightFinding{{Severity: "medium", Category: "outdated", Observation: "the draft leans on a closed gap"}}
	instance, serve := confirmedLightCapture(t, session)
	proctest.RequireStep(t, serve, "preflightVerdict")
	if serve.PendingChooser == nil || serve.PendingChooser.Kind != "agent" {
		t.Fatalf("the light verdict is the drafter's to weigh, got %+v", serve.PendingChooser)
	}
	for _, want := range []string{"outdated: the draft leans on a closed gap", "in 1 call(s)", "no rounds"} {
		if !strings.Contains(serve.Instructions, want) {
			t.Errorf("the light verdict should carry %q:\n%s", want, serve.Instructions)
		}
	}
	serve = session.Answer(t, instance, "preflightVerdict", "proceed", nil, "")
	proctest.RequireStep(t, serve, "verifySummary")
	if got := world.LLM.Calls(llm.PurposePreflight); got != 1 {
		t.Fatalf("the verdict is given once per draft version, pre-flight ran %d times", got)
	}
}

func TestCaptureVerifier_LightWithoutAWindowNamesTheKey(t *testing.T) {
	_, session := newCaptureWorld(t, "light-window", proctest.WithGroundingEnforced(), proctest.WithVerifier(sdd.VerifierPolicy{Mode: "prose"}, 0, 0))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	session.Report(t, instance, captureDraft())
	_, err := session.AnswerErr(t, instance, "playback", "confirm", nil, "write it")
	if err == nil || !strings.Contains(err.Error(), "llm.context_tokens") {
		t.Fatalf("a light call without a known window should be refused naming the key, got %v", err)
	}
}

// answerCut is the failure a provider adapter reports for an answer the model
// ended at the output token cap.
func answerCut() error {
	return &llm.Error{FinishReason: "length", Err: fmt.Errorf("gollm openai: %w: finish_reason=length, reasoning 32770 tokens, content 75122 chars", llm.ErrAnswerCut)}
}

func TestCaptureVerifier_ACutAnswerPutsTheChoiceToTheUser(t *testing.T) {
	world, session := newLightWorld(t, "light-cut")
	world.LLM.PreflightErr = answerCut()
	_, serve := confirmedLightCapture(t, session)
	proctest.RequireStep(t, serve, "preflightUnjudged")
	if serve.PendingOperation != nil || serve.PendingChooser == nil || serve.PendingChooser.Kind != "user" {
		t.Fatalf("a cut answer should be the user's choice, not a failed operation, got %+v, %+v", serve.PendingOperation, serve.PendingChooser)
	}
	for _, option := range serve.PendingChooser.Options {
		if option.Choice == "retry" {
			t.Error("a cut answer should offer no retry of the same request")
		}
	}
	for _, want := range []string{"finish_reason=length, reasoning 32770 tokens", "`llm.max_tokens`", "`llm.preflight.params`"} {
		if !strings.Contains(serve.Instructions, want) {
			t.Errorf("the serve should name the cause and the levers, missing %q:\n%s", want, serve.Instructions)
		}
	}
	if got := world.LLM.Calls(llm.PurposePreflight); got != 1 {
		t.Fatalf("a cut answer should not be retried on its own, pre-flight ran %d times", got)
	}
}

func TestCaptureVerifier_ARevisedDraftAfterACutRunsPreflightAgain(t *testing.T) {
	world, session := newLightWorld(t, "light-cut-revise")
	world.LLM.PreflightErr = answerCut()
	instance, serve := confirmedLightCapture(t, session)
	proctest.RequireStep(t, serve, "preflightUnjudged")
	world.LLM.PreflightErr = nil
	serve = session.Answer(t, instance, "preflightUnjudged", "revise", map[string]any{"body": "A tactical gap: the fixture observes something new."}, "shorter, and the cap is raised")
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, instance, "playback", "confirm", nil, "now write it")
	proctest.RequireStep(t, serve, "verifySummary")
	if got := world.LLM.Calls(llm.PurposePreflight); got != 2 {
		t.Fatalf("a revised draft goes through pre-flight again, ran %d times", got)
	}
}

func TestCaptureVerifier_ACutAnswerOutsideASkippedDeepExchangeHoldsTheWrite(t *testing.T) {
	world, session := newLightWorld(t, "light-cut-skip")
	world.LLM.PreflightErr = answerCut()
	instance, serve := confirmedLightCapture(t, session)
	proctest.RequireStep(t, serve, "preflightUnjudged")
	_, err := session.AnswerErr(t, instance, "preflightUnjudged", "skip", nil, "skip it")
	if err == nil || !strings.Contains(err.Error(), "verify.skip covers only the deep exchange") {
		t.Fatalf("a skip of a prose pre-flight should be refused, got %v", err)
	}
}
