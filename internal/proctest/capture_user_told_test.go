package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// A writing-guide finding the agent dismisses goes into its report, not to
// the user: the playback shows what the checks changed instead.
func TestCapture_TheGuideReviewNamesNoFindingToTheUser(t *testing.T) {
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
	if text := serve.Instructions; strings.Contains(text, "name to the user") || strings.Contains(text, "no playback follows") {
		t.Fatalf("guideReview should have no finding named to the user:\n%s", text)
	}
}

// A deep finding that turns on facts only the user knows goes to the user at
// the playback, never asked during the exchange: the answer step says so with
// an example.
func TestCaptureDeep_AFindingOnFactsOnlyTheUserKnowsGoesToTheUser(t *testing.T) {
	world, session := newDeepWorld(t, "deep-user-facts", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, subjectFinding("observes something."))
	serve := x.pickUp(t)
	proctest.RequireStep(t, serve, "deepAnswer")
	want := "A finding without that mark that turns on facts only the user knows, such as whether incidents were ever recorded: never accept it on your own guess, and never put it to the user here. Contest it, naming the fact only they know; if the verifier holds it, it is open for them at the playback."
	if text := serve.Instructions; !strings.Contains(text, want) || strings.Contains(text, "Ask the user") {
		t.Fatalf("deepAnswer should carry %q and ask the user nothing:\n%s", want, text)
	}
}

// A finding the verifier holds against the drafter's contest is open at the
// playback; one it withdrew is not.
func TestCaptureDeep_OnlyAHeldContestIsOpenAtThePlayback(t *testing.T) {
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
	serve := session.Answer(t, x.instance, "deepProposal", "escalate", nil, "")
	requireOpenPoint(t, serve, "- f1 [medium, missing-ref]: contested, and the verifier holds it")
	if strings.Contains(serve.Instructions, "- f2 ") {
		t.Errorf("a withdrawn finding is not open for the user:\n%s", serve.Instructions)
	}
	serve = session.Answer(t, x.instance, "playback", "confirm", nil, "they were recorded in the tracker")
	proctest.RequireStep(t, serve, "verifySummary")
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
	proctest.RequireStep(t, session.Answer(t, instance, "deepAnswer", "accept", nil, ""), "deepProposal")
	serve = session.Answer(t, instance, "deepProposal", "accept", nil, "")
	proctest.RequireStep(t, serve, "playback")
	text := serve.Instructions
	if strings.Contains(text, "Show the user") {
		t.Errorf("an autonomous playback should not address a user about the findings:\n%s", text)
	}
	for _, want := range []string{"- f1 [medium, inconsistent] accepted without an edit", "Weigh each against your grounding before you confirm or adjust the draft."} {
		if !strings.Contains(text, want) {
			t.Errorf("an autonomous playback should put the findings to the agent, missing %q:\n%s", want, text)
		}
	}
}
