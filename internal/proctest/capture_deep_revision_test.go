// A draft changed after a deep verdict goes to the verifier the exchange left
// waiting, as a revision, and the same delegate continues; a fresh verifier
// starts only where none can take it.
package proctest_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// subjectFinding is one finding whose edit rewrites old.
func subjectFinding(old string) []any {
	return []any{map[string]any{"verdict": "the gap names no subject", "axis": "inconsistent", "severity": "low", "edits": []any{deepEdit(old, old+" It names the subject.")}}}
}

// settleWithAChange accepts the verifier's findings and their proposal, then
// adjusts the draft to body at playback and confirms it: the confirm hands the
// change to the deep exchange.
func (x *deepExchange) settleWithAChange(t *testing.T, body string) *sdd.WorkflowServe {
	t.Helper()
	proctest.RequireStep(t, x.session.Answer(t, x.instance, "deepAnswer", "accept", nil, ""), "deepProposal")
	proctest.RequireStep(t, x.session.Answer(t, x.instance, "deepProposal", "accept", nil, ""), "playback")
	proctest.RequireStep(t, x.session.Answer(t, x.instance, "playback", "adjust", map[string]any{"body": body}, "one more change"), "playback")
	return x.session.Answer(t, x.instance, "playback", "confirm", nil, "now write it")
}

// pickUpRevision has the delegate pick the revision up, as the continued
// subagent does.
func (x *deepExchange) pickUpRevision(t *testing.T) *sdd.WorkflowServe {
	t.Helper()
	x.delegate.Refresh(t)
	serve := x.delegate.Report(t, x.child, nil)
	proctest.RequireStep(t, serve, "read")
	return serve
}

// pickUpFindings has the capture pick the verifier's findings up.
func (x *deepExchange) pickUpFindings(t *testing.T) *sdd.WorkflowServe {
	t.Helper()
	x.session.Refresh(t)
	return x.session.Report(t, x.instance, nil)
}

func TestCaptureDeep_AChangeAfterTheVerdictGoesToTheLiveVerifier(t *testing.T) {
	world, session := newDeepWorld(t, "deep-revision", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, subjectFinding("observes something."))
	proctest.RequireStep(t, x.pickUp(t), "deepAnswer")
	serve := x.settleWithAChange(t, "A tactical gap: the fixture observes a subject twice.")
	proctest.RequireStep(t, serve, "deepWait")
	if serve.ReportSchema != nil {
		t.Errorf("deepWait only awaits the verifier, so it should serve no report schema, got %v", serve.ReportSchema)
	}
	if len(serve.Dispatches) != 1 || serve.Dispatches[0].Session != x.dispatch.Session || !strings.Contains(serve.Dispatches[0].Instructions, "Continue the same delegate") {
		t.Fatalf("the revision should go to the same delegate, continued, got %+v", serve.Dispatches)
	}
	serve = x.pickUpRevision(t)
	for _, want := range []string{"## Revision 1 of the draft", "observes a subject twice.", "Page 1 of 1."} {
		if !strings.Contains(serve.Instructions, want) {
			t.Errorf("the revision should serve %q:\n%s", want, serve.Instructions)
		}
	}
	for _, held := range []string{"## Pre-flight's rules", "Role in the reading: declared target"} {
		if strings.Contains(serve.Instructions, held) {
			t.Errorf("the revision should leave out what the verifier holds, got %q:\n%s", held, serve.Instructions)
		}
	}
	serve = x.delegate.Answer(t, x.child, "read", "judge", map[string]any{"findings": []any{}}, "")
	proctest.RequireStep(t, serve, "waitAnswers")
	proctest.RequireStep(t, x.pickUpFindings(t), "verifySummary")
	if _, _, err := world.App.ResumeWorkflow(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: x.dispatch.Session, ClientName: "verifier"}); err != nil {
		t.Fatalf("the verifier should stay live after judging the revision, got %v", err)
	}
}

func TestCaptureDeep_ARevisionCanBeReadWhole(t *testing.T) {
	world, session := newDeepWorld(t, "deep-revision-reread", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, subjectFinding("observes something."))
	x.pickUp(t)
	proctest.RequireStep(t, x.settleWithAChange(t, "A tactical gap: the fixture observes a subject twice."), "deepWait")
	x.pickUpRevision(t)
	serve := x.delegate.Answer(t, x.child, "read", "reread", nil, "")
	if !strings.Contains(serve.Instructions, "Page 1 of 3.") || !strings.Contains(serve.Instructions, "Role in the reading: declared target") || !strings.Contains(serve.Instructions, "observes a subject twice.") {
		t.Fatalf("reread should serve the whole reading of the revised draft from page 1:\n%s", serve.Instructions)
	}
	serve = x.judge(t, []any{})
	proctest.RequireStep(t, serve, "waitAnswers")
}

// A delivery over a revision is refused until every page of the revision was
// served, as one over the first reading is.
func TestCaptureDeep_ARevisionIsJudgedOnlyOnceServed(t *testing.T) {
	read := &model.Entry{
		ID: "20260602-120000-s-tac-big", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
		Summary: "A gap whose text fills a page.", Content: strings.Repeat("A sentence of the gap read in full after the verdict.\n", 1000),
	}
	world, session := newDeepWorld(t, "deep-revision-served", sdd.VerifierPolicy{}, proctest.WithEntries(read))
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, subjectFinding("observes something."))
	x.pickUp(t)
	session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	proctest.RequireStep(t, session.Answer(t, x.instance, "deepProposal", "accept", nil, ""), "playback")
	session.Gather(t, x.instance, read.ID)
	serve := session.Answer(t, x.instance, "playback", "adjust", map[string]any{"body": "A tactical gap: the fixture observes a subject twice."}, "one more change")
	proctest.RequireStep(t, serve, "assemble")
	serve = session.Report(t, x.instance, map[string]any{"verdicts": []any{map[string]any{"ids": []any{read.ID}, "code": "other"}}})
	proctest.RequireStep(t, serve, "playback")
	proctest.RequireStep(t, session.Answer(t, x.instance, "playback", "confirm", nil, "now write it"), "deepWait")
	if _, err := x.delegate.AnswerErr(t, x.child, "read", "judge", map[string]any{"findings": []any{}}, ""); err == nil {
		t.Fatal("findings before the revision was picked up should be refused")
	}
	x.delegate.Refresh(t)
	serve = x.pickUpRevision(t)
	if !strings.Contains(serve.Instructions, "Page 1 of ") || strings.Contains(serve.Instructions, "Page 1 of 1.") {
		t.Fatalf("the entry read since the verdict should take pages of the revision:\n%s", serve.Instructions)
	}
	_, err := x.delegate.AnswerErr(t, x.child, "read", "judge", map[string]any{"findings": []any{}}, "")
	if err == nil || !strings.Contains(err.Error(), "page 1 of ") {
		t.Fatalf("findings before every revision page was served should be refused, got %v", err)
	}
	x.delegate.Refresh(t)
	serve = x.delegate.Answer(t, x.child, "read", "more", nil, "")
	if !strings.Contains(serve.Instructions, "ID: "+read.ID) {
		t.Fatalf("page 2 should serve the entry read since:\n%s", serve.Instructions)
	}
	for !strings.Contains(serve.Instructions, "Every page is served.") {
		serve = x.delegate.Answer(t, x.child, "read", "more", nil, "")
	}
	proctest.RequireStep(t, x.delegate.Answer(t, x.child, "read", "judge", map[string]any{"findings": []any{}}, ""), "waitAnswers")
	proctest.RequireStep(t, x.pickUpFindings(t), "verifySummary")
}

func TestCaptureDeep_AFreshVerifierAfterThreeRevisions(t *testing.T) {
	world, session := newDeepWorld(t, "deep-revision-cap", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, subjectFinding("observes something."))
	proctest.RequireStep(t, x.pickUp(t), "deepAnswer")
	for revision := 1; revision <= model.DeepRevisions; revision++ {
		body := fmt.Sprintf("A tactical gap: revision %d.", revision)
		proctest.RequireStep(t, x.settleWithAChange(t, body), "deepWait")
		serve := x.pickUpRevision(t)
		if !strings.Contains(serve.Instructions, fmt.Sprintf("## Revision %d of the draft", revision)) {
			t.Fatalf("revision %d should reach the same verifier:\n%s", revision, serve.Instructions)
		}
		x.delegate.Answer(t, x.child, "read", "judge", map[string]any{"findings": subjectFinding(fmt.Sprintf("revision %d.", revision))}, "")
		proctest.RequireStep(t, x.pickUpFindings(t), "deepAnswer")
	}
	proctest.RequireStep(t, x.settleWithAChange(t, "A tactical gap: one change too many."), "deepDispatch")
	fresh := dispatchDeep(t, world, session, x.instance)
	if fresh.dispatch.Session == x.dispatch.Session {
		t.Fatal("a fourth revision should go to a fresh verifier")
	}
	if _, _, err := world.App.ResumeWorkflow(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: x.dispatch.Session}); err == nil || !strings.Contains(err.Error(), "replaced") {
		t.Fatalf("the verifier past its revisions should end, replaced by the fresh one, got %v", err)
	}
}

func TestCaptureDeep_AFreshVerifierWhereNoHarnessContinuesIt(t *testing.T) {
	launcher := headlessLauncher(t)
	world, session := newDeepWorld(t, "deep-revision-headless", sdd.VerifierPolicy{}, proctest.WithDelegateLauncher(launcher))
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	forked := session.Answer(t, instance, "deepDispatch", "noSubagent", nil, "")
	round := launcher.NextRound(t)
	delegate := world.Delegate(t, &sdd.WorkflowDispatch{Session: round.Launch.Session})
	readAllPages(t, delegate, round.Launch.Instance)
	delegate.Answer(t, round.Launch.Instance, "read", "judge", map[string]any{"findings": subjectFinding("observes something.")}, "")
	round.Finish("cli-session", nil)
	x := &deepExchange{world: world, session: session, instance: instance, dispatch: forked.Dispatches[0], delegate: delegate, child: round.Launch.Instance}
	proctest.RequireStep(t, x.pickUp(t), "deepAnswer")
	proctest.RequireStep(t, x.settleWithAChange(t, "A tactical gap: the fixture observes a subject twice."), "deepDispatch")
}

// rulesLane returns the verifier's rules lane of a reading serve, which holds
// no part of the page.
func rulesLane(t *testing.T, serve *sdd.WorkflowServe) string {
	t.Helper()
	for _, lane := range serve.InstructionLanes {
		if lane.Name == "rules" {
			if strings.Contains(lane.Text, "Page ") {
				t.Fatalf("the rules lane should hold no part of the page:\n%s", lane.Text)
			}
			return lane.Text
		}
	}
	t.Fatalf("a reading serve should carry the verifier's rules as a lane of their own, got %+v", serve.InstructionLanes)
	return ""
}

// The verifier's fixed rules serve as one lane, the same on every page and
// every revision, so a proof holds them across all of them.
func TestCaptureDeep_TheVerifierRulesServeAsALaneOfTheirOwn(t *testing.T) {
	world, session := newDeepWorld(t, "deep-rules-lane", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.delegate.Refresh(t)
	first := rulesLane(t, x.delegate.Report(t, x.child, nil))
	x.judge(t, subjectFinding("observes something."))
	proctest.RequireStep(t, x.pickUp(t), "deepAnswer")
	proctest.RequireStep(t, x.settleWithAChange(t, "A tactical gap: the fixture observes a subject twice."), "deepWait")
	if revised := rulesLane(t, x.pickUpRevision(t)); revised != first {
		t.Fatalf("a revision should serve the same rules lane:\n%s", revised)
	}
}
