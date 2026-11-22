package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/test"
)

// A writing-guide finding the agent dismisses on proceed is named to the
// user before it proceeds, since no playback follows the proceed.
func TestCapture_TheGuideReviewNamesDismissedFindingsBeforeProceed(t *testing.T) {
	world, session := newCaptureWorld(t, "guide-dismissed-told")
	world.LLM.GuideFindings = []proctest.GuideFinding{{
		Reasoning: "the body leans on 'the earlier approach' without naming it",
		Axis:      "stranding", Quote: "the earlier approach", Repair: "write-in", Severity: "substantive",
	}}
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, serve.Instance, "playback", "confirm", nil, "looks right")
	proctest.RequireStep(t, serve, "guideReview")
	want := "Before you choose **proceed**, name to the user in your own words each `substantive` finding you dismiss: no playback follows."
	if text := test.Untagged(serve.Instructions); !strings.Contains(text, want) {
		t.Fatalf("guideReview should carry %q:\n%s", want, text)
	}
}

// A deep finding accepted without an edit is played back to the user before
// the write, so the user can still adjust the draft on it; the replay runs no
// second pre-flight, and the summary check names it again.
func TestCaptureDeep_AFindingAcceptedWithoutAnEditIsPlayedBackBeforeTheWrite(t *testing.T) {
	world, session := newDeepWorld(t, "deep-unedited-told", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	verdict := "the gap claims an observation the graph records nowhere"
	x.judge(t, []any{map[string]any{"verdict": verdict, "axis": "inconsistent", "severity": "medium"}})
	serve := x.pickUp(t)
	proctest.RequireStep(t, serve, "deepAnswer")
	if !strings.Contains(test.Untagged(serve.Instructions), "- f1 [medium, inconsistent]: "+verdict) {
		t.Fatalf("deepAnswer should serve the finding in full:\n%s", serve.Instructions)
	}
	preflights := world.LLM.Calls(llm.PurposePreflight)
	serve = session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	proctest.RequireStep(t, serve, "playback")
	settled := "- f1 [medium, inconsistent] accepted without an edit\n"
	replay := test.Untagged(serve.Instructions)
	for _, want := range []string{"The deep verifier left these findings without a change to the draft", settled, "Show the user each one before they confirm or adjust the draft: the finding as you were served it in full, never a paraphrase, and how it was settled."} {
		if !strings.Contains(replay, want) {
			t.Fatalf("the replayed playback should carry %q:\n%s", want, replay)
		}
	}
	serve = session.Answer(t, x.instance, "playback", "confirm", nil, "fine as it is")
	proctest.RequireStep(t, serve, "verifySummary")
	if calls := world.LLM.Calls(llm.PurposePreflight); calls != preflights {
		t.Fatalf("the replay should run no second pre-flight, got %d calls after %d", calls, preflights)
	}
	text := test.Untagged(serve.Instructions)
	for _, want := range []string{"You accepted these findings of the deep verifier, which carried no edit, so the entry was written without a change for them:", settled, "Tell the user each one."} {
		if !strings.Contains(text, want) {
			t.Fatalf("verifySummary should carry %q:\n%s", want, text)
		}
	}
	for name, text := range map[string]string{"playback": replay, "verifySummary": text} {
		if strings.Contains(text, verdict) {
			t.Errorf("%s should name the settled finding in one line, not repeat it in full:\n%s", name, text)
		}
	}
}

// A playback round carrying the deep exchange's settled findings is the only
// one: the round after an adjust leaves them out.
func TestCaptureDeep_SettledFindingsRideOnePlaybackRound(t *testing.T) {
	world, session := newDeepWorld(t, "deep-settled-once", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{map[string]any{"verdict": "the gap claims an observation the graph records nowhere", "axis": "inconsistent", "severity": "medium"}})
	proctest.RequireStep(t, x.pickUp(t), "deepAnswer")
	serve := session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	proctest.RequireStep(t, serve, "playback")
	const settled = "- f1 [medium, inconsistent] accepted without an edit"
	if !strings.Contains(test.Untagged(serve.Instructions), settled) {
		t.Fatalf("the first playback after the exchange should carry the settled finding:\n%s", serve.Instructions)
	}
	serve = session.Answer(t, x.instance, "playback", "adjust", map[string]any{"body": "A tactical gap: the fixture observes one thing."}, "say one thing")
	proctest.RequireStep(t, serve, "playback")
	if text := test.Untagged(serve.Instructions); strings.Contains(text, settled) || strings.Contains(text, "left these findings without a change") {
		t.Fatalf("a playback round after the one that carried the settled findings should leave them out:\n%s", text)
	}
}

// A deep finding that turns on facts only the user knows goes to the user:
// the answer step says so with an example.
func TestCaptureDeep_AFindingOnFactsOnlyTheUserKnowsGoesToTheUser(t *testing.T) {
	world, session := newDeepWorld(t, "deep-user-facts", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, subjectFinding("observes something."))
	serve := x.pickUp(t)
	proctest.RequireStep(t, serve, "deepAnswer")
	want := "A finding without that mark that turns on facts only the user knows, such as whether incidents were ever recorded: never accept or contest it on your own guess. Ask the user; when their answer refutes it, contest it, quoting them in the reason."
	if text := test.Untagged(serve.Instructions); !strings.Contains(text, want) {
		t.Fatalf("deepAnswer should carry %q:\n%s", want, text)
	}
}

// A finding the verifier holds against the drafter's contest goes to the user
// and is never named as taken without an edit, nor is one it withdrew.
func TestCaptureDeep_OnlyAFindingTakenWithoutAnEditIsNamedSo(t *testing.T) {
	world, session := newDeepWorld(t, "deep-unedited-only", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{
		map[string]any{"verdict": "the gap rests on incidents no entry records", "axis": "missing-ref", "severity": "medium"},
		map[string]any{"verdict": "the gap names no owner", "axis": "inconsistent", "severity": "low"},
	})
	x.pickUp(t)
	session.Answer(t, x.instance, "deepAnswer", "contest", map[string]any{"deepContest": []any{
		map[string]any{"target": "f1", "verdict": "contest", "reason": "the user named the incidents"},
		map[string]any{"target": "f2", "verdict": "contest", "reason": "the owner is the team"},
	}}, "")
	x.delegate.Refresh(t)
	x.delegate.Report(t, x.child, nil)
	x.delegate.Answer(t, x.child, "respond", "respond", map[string]any{"responses": []any{
		map[string]any{"target": "f1", "outcome": "held", "note": "no entry records them"},
		map[string]any{"target": "f2", "outcome": "dropped", "note": "the team owns it"},
	}}, "")
	session.Refresh(t)
	proctest.RequireStep(t, session.Report(t, x.instance, nil), "deepProposal")
	proctest.RequireStep(t, session.Answer(t, x.instance, "deepProposal", "escalate", nil, ""), "deepUser")
	serve := session.Answer(t, x.instance, "deepUser", "settle", nil, "they were recorded in the tracker")
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, x.instance, "playback", "confirm", nil, "fine as it is")
	proctest.RequireStep(t, serve, "verifySummary")
	if text := test.Untagged(serve.Instructions); strings.Contains(text, "which carried no edit") {
		t.Fatalf("a held or withdrawn finding is not one taken without an edit:\n%s", text)
	}
}

// No user reads an autonomous playback, so a deep finding accepted without an
// edit is put to the agent's own check, not shown to a user.
func TestCaptureDeep_AutonomousPlaybackPutsUneditedFindingsToTheAgent(t *testing.T) {
	world, session := newDeepWorld(t, "deep-unedited-autonomous", sdd.VerifierPolicy{}, proctest.WithAutonomousDialogue(), clientActor("deep-unedited-autonomous"))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	x := dispatchDeep(t, world, session, instance)
	verdict := "the gap claims an observation the graph records nowhere"
	x.judge(t, []any{map[string]any{"verdict": verdict, "axis": "inconsistent", "severity": "medium"}})
	proctest.RequireStep(t, x.pickUp(t), "deepAnswer")
	serve = session.Answer(t, instance, "deepAnswer", "accept", nil, "")
	proctest.RequireStep(t, serve, "playback")
	text := test.Untagged(serve.Instructions)
	if strings.Contains(text, "Show the user") {
		t.Errorf("an autonomous playback should not address a user about the findings:\n%s", text)
	}
	for _, want := range []string{"- f1 [medium, inconsistent] accepted without an edit", "Weigh each against your grounding before you confirm or adjust the draft."} {
		if !strings.Contains(text, want) {
			t.Errorf("an autonomous playback should put the findings to the agent, missing %q:\n%s", want, text)
		}
	}
}
