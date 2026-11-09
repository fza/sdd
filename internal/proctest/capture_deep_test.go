// Capture with the deep draft verifier (20261109-004729-d-cpt-xs5, slice 3):
// once pre-flight leaves no high finding open, the capture dispatches the
// verify task into a delegate child session, exchanges findings, answers and
// responses with it, and writes only under a deep verdict or the user's skip
// covering the current draft.
package proctest_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

const deepBody = "A tactical gap: the fixture observes something."

func newDeepWorld(t *testing.T, connID string, policy sdd.VerifierPolicy, options ...proctest.Option) (*proctest.World, *proctest.Session) {
	t.Helper()
	policy.Mode = "deep"
	return newCaptureWorld(t, connID, append([]proctest.Option{proctest.WithGroundingEnforced(),
		proctest.WithVerifier(policy, 200000, 4096)}, options...)...)
}

// deepExchange is one capture driven to the deep exchange, with its verify
// delegate.
type deepExchange struct {
	world    *proctest.World
	session  *proctest.Session
	instance string
	dispatch *sdd.WorkflowDispatch
	delegate *proctest.Session
	child    string
}

// confirmedDeepCapture drafts, confirms the playback and passes pre-flight,
// reaching the deep exchange's dispatch.
func confirmedDeepCapture(t *testing.T, session *proctest.Session, draft map[string]any) (string, *sdd.WorkflowServe) {
	t.Helper()
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
	return instance, session.Answer(t, instance, "playback", "confirm", nil, "write it")
}

// dispatchDeep answers the dispatch and starts the verify delegate.
func dispatchDeep(t *testing.T, world *proctest.World, session *proctest.Session, instance string) *deepExchange {
	t.Helper()
	serve := session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	proctest.RequireStep(t, serve, "deepLaunch")
	forked := session.StartChild(t, "verify", nil, instance)
	if len(forked.Dispatches) != 1 {
		t.Fatalf("starting the verify procedure should owe one dispatch, got %+v", forked.Dispatches)
	}
	x := &deepExchange{world: world, session: session, instance: instance, dispatch: forked.Dispatches[0]}
	x.delegate = world.Delegate(t, x.dispatch)
	x.child = x.dispatch.Instance
	return x
}

// startDeep reaches the dispatch and starts the verify delegate.
func startDeep(t *testing.T, world *proctest.World, session *proctest.Session, draft map[string]any) *deepExchange {
	t.Helper()
	instance, serve := confirmedDeepCapture(t, session, draft)
	proctest.RequireStep(t, serve, "deepDispatch")
	return dispatchDeep(t, world, session, instance)
}

// judge serves the delegate every page and reports its findings.
func (x *deepExchange) judge(t *testing.T, findings []any) *sdd.WorkflowServe {
	t.Helper()
	serve := x.delegate.Answer(t, x.child, "read", "more", nil, "")
	proctest.RequireStep(t, serve, "read")
	return x.delegate.Answer(t, x.child, "read", "judge", map[string]any{"findings": findings}, "")
}

// pickUp reports the launch started and picks the delegate's delivery up.
func (x *deepExchange) pickUp(t *testing.T) *sdd.WorkflowServe {
	t.Helper()
	x.session.Refresh(t)
	return x.session.Answer(t, x.instance, "deepLaunch", "started", nil, "")
}

func deepEdit(old, new string) map[string]any {
	return map[string]any{"old": old, "new": new}
}

func TestCaptureDeep_ACleanVerdictWrites(t *testing.T) {
	world, session := newDeepWorld(t, "deep-clean", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	for _, prompt := range []string{x.dispatch.Prompt, x.dispatch.ContinuePrompt} {
		if strings.Contains(prompt, string(session.ID)) || !strings.Contains(prompt, string(x.dispatch.Session)) {
			t.Fatalf("a delegate prompt carries the child handle and never the capture's, got %q", prompt)
		}
	}
	serve := x.judge(t, []any{})
	proctest.RequireStep(t, serve, "waitAnswers")
	serve = x.pickUp(t)
	proctest.RequireStep(t, serve, "verifySummary")
	serve = session.Answer(t, x.instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	entryID, _ := serve.Produced["entryId"].(string)
	if entry := proctest.LoadEntry(t, world.GraphDir, entryID); entry.Verification != "" || entry.Confidence != "medium" {
		t.Fatalf("a verified entry carries no skip marker and keeps its confidence, got %q and %q", entry.Verification, entry.Confidence)
	}
}

func TestCaptureDeep_ServesTheReadingInPagesAndRefusesEarlyFindings(t *testing.T) {
	world, session := newDeepWorld(t, "deep-pages", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	_, resume := world.Resume(t, x.dispatch.Session, "verifier")
	first := resume.Open[0].Instructions
	x.delegate.Refresh(t)
	for _, want := range []string{"Page 1 of 2.", "## Draft", deepBody, "Role in the reading: declared target", "do not bind you"} {
		if !strings.Contains(first, want) {
			t.Errorf("page 1 should carry %q:\n%s", want, first)
		}
	}
	_, err := x.delegate.AnswerErr(t, x.child, "read", "judge", map[string]any{"findings": []any{}}, "")
	if err == nil || !strings.Contains(err.Error(), "page 1 of 2 is the last served") {
		t.Fatalf("findings before every page was served should be refused, got %v", err)
	}
	serve := x.delegate.Answer(t, x.child, "read", "more", nil, "")
	if !strings.Contains(serve.Instructions, "Page 2 of 2.") || !strings.Contains(serve.Instructions, "ID: "+captureRef2ID) {
		t.Fatalf("page 2 should carry the briefs:\n%s", serve.Instructions)
	}
}

func TestCaptureDeep_AVerdictReportsItsFindingsList(t *testing.T) {
	world, session := newDeepWorld(t, "deep-findings-list", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.delegate.Answer(t, x.child, "read", "more", nil, "")
	_, err := x.delegate.AnswerErr(t, x.child, "read", "judge", nil, "")
	if err == nil || !strings.Contains(err.Error(), "report findings: the whole list") {
		t.Fatalf("a verdict without a findings list should be refused, got %v", err)
	}
}

func TestCaptureDeep_ContestRoundBuildsTheProposal(t *testing.T) {
	world, session := newDeepWorld(t, "deep-contest", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{
		map[string]any{"verdict": "the gap names no subject", "axis": "inconsistent", "severity": "medium", "edits": []any{deepEdit("observes something.", "observes the attachment writer dropping a file.")}},
		map[string]any{"verdict": "the layer is too low", "axis": "ref-fit", "severity": "low", "edits": []any{deepEdit("layer: \"tactical\"", "layer: \"conceptual\"")}},
	})
	serve := x.pickUp(t)
	proctest.RequireStep(t, serve, "deepAnswer")
	if !strings.Contains(serve.Instructions, "f1 [medium, inconsistent]: the gap names no subject") || strings.Contains(serve.Instructions, "attachment writer") {
		t.Fatalf("the findings should be served for answering, their edits held back:\n%s", serve.Instructions)
	}
	for _, tc := range []struct {
		contest []any
		refusal string
	}{
		{[]any{map[string]any{"target": "f2", "verdict": "contest", "reason": "r"}, map[string]any{"target": "f2", "verdict": "accept"}}, "f2 is answered twice"},
		{[]any{map[string]any{"target": "f2", "verdict": "accept"}}, "a contest names at least one finding with verdict contest"},
		{[]any{map[string]any{"target": "f3", "verdict": "contest", "reason": "r"}}, "f3 names no finding: the verifier raised 2"},
	} {
		_, err := session.AnswerErr(t, x.instance, "deepAnswer", "contest", map[string]any{"deepContest": tc.contest}, "")
		if err == nil || !strings.Contains(err.Error(), tc.refusal) {
			t.Fatalf("the contest should be refused with %q, got %v", tc.refusal, err)
		}
		session.Refresh(t)
	}
	serve = session.Answer(t, x.instance, "deepAnswer", "contest", map[string]any{"deepContest": []any{
		map[string]any{"target": "f2", "verdict": "contest", "reason": "the gap is about one command, a tactical matter"},
	}}, "")
	proctest.RequireStep(t, serve, "deepResponding")
	if len(serve.Dispatches) != 1 || serve.Dispatches[0].Session != x.dispatch.Session {
		t.Fatalf("the contest should be handed to the same delegate, got %+v", serve.Dispatches)
	}

	x.delegate.Refresh(t)
	serve = x.delegate.Report(t, x.child, nil)
	proctest.RequireStep(t, serve, "respond")
	if !strings.Contains(serve.Instructions, "- f2: the gap is about one command, a tactical matter") || strings.Contains(serve.Instructions, "- f1:") {
		t.Fatalf("the delegate should be served the contested findings only:\n%s", serve.Instructions)
	}
	_, err := x.delegate.AnswerErr(t, x.child, "respond", "respond", map[string]any{"responses": []any{map[string]any{"target": "f1", "outcome": "held", "note": "n"}}}, "")
	if err == nil || !strings.Contains(err.Error(), "f1 is not contested") {
		t.Fatalf("a response to an uncontested finding should be refused, got %v", err)
	}
	serve = x.delegate.Answer(t, x.child, "respond", "respond", map[string]any{"responses": []any{
		map[string]any{"target": "f2", "outcome": "dropped", "note": "the drafter's reason holds"},
	}}, "")
	proctest.RequireStep(t, serve, "waitAnswers")

	session.Refresh(t)
	serve = session.Report(t, x.instance, nil)
	proctest.RequireStep(t, serve, "deepProposal")
	if !strings.Contains(serve.Instructions, "@@ h1 [f1 applied] line") || strings.Contains(serve.Instructions, "conceptual") || !strings.Contains(serve.Instructions, "after 2 of 3 verifier deliveries") {
		t.Fatalf("the proposal should carry the accepted finding's hunk only, labelled by it:\n%s", serve.Instructions)
	}
	_, err = session.AnswerErr(t, x.instance, "deepProposal", "accept", map[string]any{"body": deepBody + " Something else."}, "")
	if err == nil || !strings.Contains(err.Error(), "the draft differs from the proposal") {
		t.Fatalf("a draft other than the proposal should be refused, got %v", err)
	}
	serve = session.Answer(t, x.instance, "deepProposal", "accept", map[string]any{"body": "A tactical gap: the fixture observes the attachment writer dropping a file."}, "")
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, x.instance, "playback", "confirm", nil, "yes, with the verifier's change")
	proctest.RequireStep(t, serve, "verifySummary")
}

func TestCaptureDeep_OwnerFindingGoesToTheUser(t *testing.T) {
	world, session := newDeepWorld(t, "deep-owner", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{map[string]any{"verdict": "whether this gap is in scope is the owner's call", "axis": "aspiration", "severity": "medium", "owner": true}})
	serve := x.pickUp(t)
	proctest.RequireStep(t, serve, "deepAnswer")
	if !strings.Contains(serve.Instructions, "only the user can settle it") {
		t.Fatalf("an owner finding should be named as the user's:\n%s", serve.Instructions)
	}
	_, err := session.AnswerErr(t, x.instance, "deepAnswer", "contest", map[string]any{"deepContest": []any{map[string]any{"target": "f1", "verdict": "contest", "reason": "in scope"}}}, "")
	if err == nil || !strings.Contains(err.Error(), "f1 is the user's to settle") {
		t.Fatalf("an owner finding should be refused in a contest, got %v", err)
	}
	session.Refresh(t)
	serve = session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	proctest.RequireStep(t, serve, "deepProposal")
	serve = session.Answer(t, x.instance, "deepProposal", "accept", nil, "")
	proctest.RequireStep(t, serve, "deepUser")
	if serve.PendingChooser == nil || serve.PendingChooser.Kind != "user" || !strings.Contains(serve.Instructions, "f1 (only the user can settle it)") {
		t.Fatalf("the open point should go to the user, got %+v:\n%s", serve.PendingChooser, serve.Instructions)
	}
	serve = session.Answer(t, x.instance, "deepUser", "settle", nil, "it is in scope")
	proctest.RequireStep(t, serve, "verifySummary")
}

func TestCaptureDeep_ThreeDeliveriesThenTheUser(t *testing.T) {
	world, session := newDeepWorld(t, "deep-cap", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{map[string]any{"verdict": "the gap names no subject", "axis": "inconsistent", "severity": "medium", "edits": []any{deepEdit("observes something.", "observes a subject.")}}})
	x.pickUp(t)
	contest := map[string]any{"deepContest": []any{map[string]any{"target": "f1", "verdict": "contest", "reason": "the subject is in the ref"}}}
	hold := map[string]any{"responses": []any{map[string]any{"target": "f1", "outcome": "held", "note": "a reader of the entry alone cannot tell"}}}
	chooser := "deepAnswer"
	for round := 2; round <= 3; round++ {
		session.Answer(t, x.instance, chooser, "contest", contest, "")
		x.delegate.Refresh(t)
		x.delegate.Report(t, x.child, nil)
		x.delegate.Answer(t, x.child, "respond", "respond", hold, "")
		session.Refresh(t)
		serve := session.Report(t, x.instance, nil)
		proctest.RequireStep(t, serve, "deepProposal")
		chooser = "deepProposal"
	}
	_, err := session.AnswerErr(t, x.instance, "deepProposal", "contest", contest, "")
	if err == nil || !strings.Contains(err.Error(), "made its 3 deliveries") {
		t.Fatalf("a contest after the third delivery should be refused, got %v", err)
	}
	session.Refresh(t)
	serve := session.Answer(t, x.instance, "deepProposal", "escalate", nil, "")
	proctest.RequireStep(t, serve, "deepUser")
	if !strings.Contains(serve.Instructions, "the verifier holds") || !strings.Contains(serve.Instructions, "the drafter contests: the subject is in the ref") {
		t.Fatalf("the user should see both positions of the open point:\n%s", serve.Instructions)
	}
}

func TestCaptureDeep_RefusedRepairIsAVerifierFailure(t *testing.T) {
	world, session := newDeepWorld(t, "deep-failure", sdd.VerifierPolicy{})
	draft := captureDraft()
	draft["confidence"] = "high"
	x := startDeep(t, world, session, draft)
	bad := []any{map[string]any{"verdict": "v", "axis": "contradicts", "severity": "high", "edits": []any{deepEdit("text the draft lacks", "x")}}}
	serve := x.judge(t, bad)
	proctest.RequireStep(t, serve, "repair")
	if !strings.Contains(serve.Instructions, "old text matches the draft 0 times") {
		t.Fatalf("the refusal should name the edit that does not apply:\n%s", serve.Instructions)
	}
	serve = x.delegate.Answer(t, x.child, "repair", "repair", map[string]any{"findings": bad}, "")
	proctest.RequireStatus(t, serve, "completed")
	serve = x.pickUp(t)
	proctest.RequireStep(t, serve, "deepFailed")
	if serve.PendingChooser == nil || serve.PendingChooser.Kind != "user" || !strings.Contains(serve.Instructions, "refused after the repair") {
		t.Fatalf("a verifier failure is the user's to retry or skip, got %+v:\n%s", serve.PendingChooser, serve.Instructions)
	}
	serve = session.Answer(t, x.instance, "deepFailed", "skip", nil, "skip it this once")
	proctest.RequireStep(t, serve, "verifySummary")
	serve = session.Answer(t, x.instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	entryID, _ := serve.Produced["entryId"].(string)
	if entry := proctest.LoadEntry(t, world.GraphDir, entryID); entry.Verification != model.VerificationSkipped || entry.Confidence != "low" {
		t.Fatalf("a failure skip writes verification: skipped and confidence low, got %q and %q", entry.Verification, entry.Confidence)
	}
}

func TestCaptureDeep_SkipNeverKeepsTheCaptureWaiting(t *testing.T) {
	world, session := newDeepWorld(t, "deep-never", sdd.VerifierPolicy{Skip: "never"})
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	serve = session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	proctest.RequireStep(t, serve, "deepLaunch")
	serve = session.Answer(t, instance, "deepLaunch", "failed", map[string]any{"deepLaunchFailure": "no agent CLI answered"}, "")
	proctest.RequireStep(t, serve, "deepFailed")
	if !strings.Contains(serve.Instructions, "The launch failed: no agent CLI answered") {
		t.Fatalf("the failure should be served in full:\n%s", serve.Instructions)
	}
	_, err := session.AnswerErr(t, instance, "deepFailed", "skip", nil, "skip it")
	if err == nil || !strings.Contains(err.Error(), "verifier.skip is never") {
		t.Fatalf("a skip under verifier.skip: never should be refused, got %v", err)
	}
	_ = world
}

func TestCaptureDeep_WeighedSkipCapsConfidenceAtMedium(t *testing.T) {
	world, session := newDeepWorld(t, "deep-weighed", sdd.VerifierPolicy{Skip: "weighed"})
	draft := captureDraft()
	draft["confidence"] = "high"
	instance, serve := confirmedDeepCapture(t, session, draft)
	proctest.RequireStep(t, serve, "deepWeigh")
	if serve.PendingChooser == nil || serve.PendingChooser.Kind != "user" || !strings.Contains(serve.Instructions, "Recommended: skip") {
		t.Fatalf("the user weighs the run with sdd's recommendation, got %+v:\n%s", serve.PendingChooser, serve.Instructions)
	}
	serve = session.Answer(t, instance, "deepWeigh", "skip", nil, "not worth it")
	proctest.RequireStep(t, serve, "verifySummary")
	serve = session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	entryID, _ := serve.Produced["entryId"].(string)
	if entry := proctest.LoadEntry(t, world.GraphDir, entryID); entry.Verification != model.VerificationSkipped || entry.Confidence != "medium" {
		t.Fatalf("a weighed skip writes verification: skipped and caps confidence at medium, got %q and %q", entry.Verification, entry.Confidence)
	}
}

func TestCaptureDeep_WeighingRecommendsRunForACheckablePath(t *testing.T) {
	checkout := fstest.MapFS{"internal/engine/spec.go": {Data: []byte("package engine")}}
	_, session := newDeepWorld(t, "deep-weigh-run", sdd.VerifierPolicy{Skip: "weighed"}, proctest.WithCheckout(checkout))
	draft := captureDraft()
	draft["body"] = "A tactical gap: `internal/engine/spec.go` refuses the fixture."
	_, serve := confirmedDeepCapture(t, session, draft)
	proctest.RequireStep(t, serve, "deepWeigh")
	if !strings.Contains(serve.Instructions, "Recommended: run, because it names internal/engine/spec.go, a path in the checkout") {
		t.Fatalf("a path found in the checkout should recommend the run:\n%s", serve.Instructions)
	}
}

func TestCaptureDeep_OverriddenHighFindingIsSettledForTheVerifier(t *testing.T) {
	world, session := newDeepWorld(t, "deep-override", sdd.VerifierPolicy{})
	world.LLM.PreflightFindings = []proctest.PreflightFinding{{Severity: "high", Category: "duplicates", Observation: "the draft repeats a live entry"}}
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "reviseOrOverride")
	serve = session.Answer(t, instance, "reviseOrOverride", "override", nil, "it is no duplicate")
	proctest.RequireStep(t, serve, "deepDispatch")
	x := dispatchDeep(t, world, session, instance)
	_, resume := world.Resume(t, x.dispatch.Session, "verifier")
	if page := resume.Open[0].Instructions; !strings.Contains(page, "duplicates: the draft repeats a live entry") || !strings.Contains(page, "The user overrode the high ones: they are settled.") {
		t.Fatalf("the deep verifier should receive pre-flight's findings, the override as settled:\n%s", page)
	}
}

func TestCaptureDeep_AutonomousVerifiesBeforePlayback(t *testing.T) {
	world, session := newDeepWorld(t, "deep-autonomous", sdd.VerifierPolicy{}, proctest.WithAutonomousDialogue())
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	x := dispatchDeep(t, world, session, instance)
	x.judge(t, []any{})
	serve = x.pickUp(t)
	proctest.RequireStep(t, serve, "playback")
}

// fallbackLauncher stands in for the headless launches: it reports the CLI
// it ran and drives the delegate's first round the way that CLI would.
type fallbackLauncher struct {
	t        *testing.T
	world    *proctest.World
	launches []sdd.DelegateLaunch
}

func (l *fallbackLauncher) LaunchDelegate(_ context.Context, launch sdd.DelegateLaunch) (sdd.DelegateLaunched, error) {
	if launch.Launch == "" {
		return sdd.DelegateLaunched{}, sdd.ErrDelegateNotLaunched
	}
	l.launches = append(l.launches, launch)
	delegate := l.world.Delegate(l.t, &sdd.WorkflowDispatch{Session: launch.Session})
	delegate.Answer(l.t, launch.Instance, "read", "more", nil, "")
	delegate.Answer(l.t, launch.Instance, "read", "judge", map[string]any{"findings": []any{}}, "")
	return sdd.DelegateLaunched{Session: "cli-session", Launch: "claude"}, nil
}

func TestCaptureDeep_NoSubagentFallsBackToAHeadlessLaunch(t *testing.T) {
	launcher := &fallbackLauncher{t: t}
	world, session := newDeepWorld(t, "deep-fallback", sdd.VerifierPolicy{}, proctest.WithDelegateLauncher(launcher))
	launcher.world = world
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "noSubagent", nil, "")
	forked := session.StartChild(t, "verify", nil, instance)
	if len(launcher.launches) != 1 || launcher.launches[0].Launch != sdd.DelegateLaunchFallback || launcher.launches[0].Project != "proctest" {
		t.Fatalf("a harness without a subagent tool should get the headless fallback, got %+v", launcher.launches)
	}
	if strings.Contains(launcher.launches[0].Prompt, string(session.ID)) {
		t.Fatal("the launched prompt never carries the capture's session handle")
	}
	if dispatch := forked.Dispatches[0]; dispatch.Launched != "cli-session" || !strings.Contains(dispatch.Instructions, "fell back to the headless claude launch") {
		t.Fatalf("the serve should name the launch it fell back to, got %+v", dispatch)
	}
	session.Refresh(t)
	serve = session.Answer(t, instance, "deepLaunch", "started", nil, "")
	proctest.RequireStep(t, serve, "verifySummary")
}

func TestCaptureDeep_ConfiguredHeadlessLaunchRunsThroughSdd(t *testing.T) {
	launcher := &fallbackLauncher{t: t}
	world, session := newDeepWorld(t, "deep-headless", sdd.VerifierPolicy{Launch: "codex"}, proctest.WithDelegateLauncher(launcher))
	launcher.world = world
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	forked := session.StartChild(t, "verify", nil, instance)
	if len(launcher.launches) != 1 || launcher.launches[0].Launch != "codex" || launcher.launches[0].Round != 1 {
		t.Fatalf("a configured headless launch should go to sdd's launcher, got %+v", launcher.launches)
	}
	if dispatch := forked.Dispatches[0]; dispatch.Launched != "cli-session" || !strings.Contains(dispatch.Instructions, "sdd launched the delegate itself, so launch nothing") {
		t.Fatalf("the serve should tell the harness to launch nothing, got %+v", dispatch)
	}
}

func TestCaptureDeep_SubagentLaunchStaysWithTheHarness(t *testing.T) {
	launcher := &fallbackLauncher{t: t}
	world, session := newDeepWorld(t, "deep-subagent", sdd.VerifierPolicy{}, proctest.WithDelegateLauncher(launcher))
	launcher.world = world
	x := startDeep(t, world, session, captureDraft())
	if len(launcher.launches) != 0 || x.dispatch.Launched != "" || !strings.Contains(x.dispatch.Instructions, "Launch a fresh-context sub-agent") {
		t.Fatalf("a subagent launch is the harness's, got %+v and %d launches", x.dispatch, len(launcher.launches))
	}
}

func TestCaptureDeep_EditAfterTheVerdictRunsADeltaExchange(t *testing.T) {
	world, session := newDeepWorld(t, "deep-delta", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{map[string]any{"verdict": "the gap names no subject", "axis": "inconsistent", "severity": "low", "edits": []any{deepEdit("observes something.", "observes a subject.")}}})
	x.pickUp(t)
	session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	serve := session.Answer(t, x.instance, "deepProposal", "accept", map[string]any{"body": "A tactical gap: the fixture observes a subject."}, "")
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, x.instance, "playback", "adjust", map[string]any{"body": "A tactical gap: the fixture observes a subject twice."}, "one more word")
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, x.instance, "playback", "confirm", nil, "now write it")
	proctest.RequireStep(t, serve, "deepDispatch")
	again := dispatchDeep(t, world, session, x.instance)
	_, resume := world.Resume(t, again.dispatch.Session, "verifier")
	if page := resume.Open[0].Instructions; !strings.Contains(page, "## Changed since the last verdict") || !strings.Contains(page, "observes a subject twice.") {
		t.Fatalf("an edit after the verdict should get a delta reading of the changed passage:\n%s", page)
	}
	if _, _, err := world.App.ResumeWorkflow(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: x.dispatch.Session}); err == nil || !strings.Contains(err.Error(), "replaced") {
		t.Fatalf("the earlier exchange's delegate should end, replaced by the new one, got %v", err)
	}
}

// failingLauncher refuses its first headless launch, then drives the
// delegate like fallbackLauncher.
type failingLauncher struct {
	fallbackLauncher
	failed []sdd.SessionID
}

func (l *failingLauncher) LaunchDelegate(ctx context.Context, launch sdd.DelegateLaunch) (sdd.DelegateLaunched, error) {
	if launch.Launch != "" && len(l.failed) == 0 {
		l.failed = append(l.failed, launch.Session)
		return sdd.DelegateLaunched{}, errors.New("codex failed: exit status 1: not logged in")
	}
	return l.fallbackLauncher.LaunchDelegate(ctx, launch)
}

func TestCaptureDeep_FailedLaunchEndsItsDelegateAndRetries(t *testing.T) {
	launcher := &failingLauncher{fallbackLauncher: fallbackLauncher{t: t}}
	world, session := newDeepWorld(t, "deep-launch-failed", sdd.VerifierPolicy{Launch: "codex"}, proctest.WithDelegateLauncher(launcher))
	launcher.world = world
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	_, err := session.StartChildErr(t, "verify", nil, instance)
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("a failed launch should fail the start with the launcher's error, got %v", err)
	}
	_, _, err = world.App.ResumeWorkflow(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: launcher.failed[0], ClientName: "verifier"})
	if err == nil || !strings.Contains(err.Error(), "the delegate's launch failed") {
		t.Fatalf("the delegate of a failed launch should be torn down, got %v", err)
	}
	session.Refresh(t)
	serve = session.Answer(t, instance, "deepLaunch", "failed", map[string]any{"deepLaunchFailure": err.Error()}, "")
	proctest.RequireStep(t, serve, "deepFailed")
	serve = session.Answer(t, instance, "deepFailed", "retry", nil, "try again")
	proctest.RequireStep(t, serve, "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	session.StartChild(t, "verify", nil, instance)
	session.Refresh(t)
	serve = session.Answer(t, instance, "deepLaunch", "started", nil, "")
	proctest.RequireStep(t, serve, "verifySummary")
}

func TestCaptureDeep_HeadlessLaunchWithoutALauncherIsRefused(t *testing.T) {
	_, session := newDeepWorld(t, "deep-no-launcher", sdd.VerifierPolicy{Launch: "claude"})
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	_, err := session.StartChildErr(t, "verify", nil, instance)
	if err == nil || !strings.Contains(err.Error(), "verifier.launch claude needs sdd to launch the delegate") {
		t.Fatalf("a headless launch on a server that launches nothing should be refused naming the key, got %v", err)
	}
}

func TestCaptureDeep_ARetriedExchangeReplacesTheWaitingDelegate(t *testing.T) {
	world, session := newDeepWorld(t, "deep-replace", sdd.VerifierPolicy{})
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	first := dispatchDeep(t, world, session, instance)
	serve = session.Answer(t, instance, "deepLaunch", "failed", map[string]any{"deepLaunchFailure": "the subagent tool errored"}, "")
	proctest.RequireStep(t, serve, "deepFailed")
	serve = session.Answer(t, instance, "deepFailed", "retry", nil, "try again")
	proctest.RequireStep(t, serve, "deepDispatch")
	second := dispatchDeep(t, world, session, instance)
	if second.dispatch.Session == first.dispatch.Session {
		t.Fatal("a retried exchange should dispatch a new delegate")
	}
	_, _, err := world.App.ResumeWorkflow(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: first.dispatch.Session, ClientName: "verifier"})
	if err == nil || !strings.Contains(err.Error(), "a new deep exchange replaced it") {
		t.Fatalf("the waiting delegate of the earlier exchange should be torn down, got %v", err)
	}
}

func TestCaptureDeep_EveryContestedFindingGetsAResponse(t *testing.T) {
	world, session := newDeepWorld(t, "deep-every-response", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{
		map[string]any{"verdict": "the gap names no subject", "axis": "inconsistent", "severity": "medium", "edits": []any{deepEdit("observes something.", "observes a subject.")}},
		map[string]any{"verdict": "the layer is too low", "axis": "ref-fit", "severity": "low", "edits": []any{deepEdit("layer: \"tactical\"", "layer: \"conceptual\"")}},
	})
	x.pickUp(t)
	session.Answer(t, x.instance, "deepAnswer", "contest", map[string]any{"deepContest": []any{
		map[string]any{"target": "f1", "verdict": "contest", "reason": "the subject is named"},
		map[string]any{"target": "f2", "verdict": "contest", "reason": "a tactical matter"},
	}}, "")
	x.delegate.Refresh(t)
	x.delegate.Report(t, x.child, nil)
	_, err := x.delegate.AnswerErr(t, x.child, "respond", "respond", map[string]any{"responses": []any{map[string]any{"target": "f2", "outcome": "conceded", "note": "tactical it is"}}}, "")
	if err == nil || !strings.Contains(err.Error(), "f1 is contested and has no response") {
		t.Fatalf("a contested finding left without a response should be refused, got %v", err)
	}
}

func TestCaptureDeep_StartedNeedsADispatchedDelegate(t *testing.T) {
	_, session := newDeepWorld(t, "deep-no-fork", sdd.VerifierPolicy{})
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	_, err := session.AnswerErr(t, instance, "deepLaunch", "started", nil, "")
	if err == nil || !strings.Contains(err.Error(), "no verify delegate was dispatched for this exchange yet") {
		t.Fatalf("started without a dispatched delegate should be refused, got %v", err)
	}
}
