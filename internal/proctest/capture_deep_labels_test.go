package proctest_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/test"
)

// The entry a verifier's finding rests on is referenced from the entry the
// capture writes, as a live label.
func TestCaptureDeep_AFindingsBasisIsALiveReference(t *testing.T) {
	store := laneLedger(t, false)
	world, session := newDeepWorld(t, "deep-basis", sdd.VerifierPolicy{}, proctest.WithRelevance(store))
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{
		map[string]any{"verdict": "the gap restates the raw signal", "axis": "duplicates", "severity": "low", "basis": []any{captureRef2ID},
			"edits": []any{deepEdit("observes something.", "observes something the raw signal does not.")}},
	})
	x.pickUp(t)
	session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	session.Answer(t, x.instance, "deepProposal", "accept", nil, "")
	session.Answer(t, x.instance, "playback", "confirm", nil, "yes, with the verifier's changes")
	serve := session.Answer(t, x.instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	entryID, _ := serve.Produced["entryId"].(string)
	segments, err := store.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ledger.Reader(model.NewRelevancePair(entryID, captureRef2ID))
	if !ok || got.Code != model.ReaderReferenced || got.Source != model.SourceLive {
		t.Fatalf("the finding's basis = %+v: want a live reference from the written entry", got)
	}
}

// A hidden entry the verifier opens is an audit miss when a finding rests on
// it; opened alone it counts toward the status's opened-hidden line and
// leaves the drift window alone.
func TestCaptureDeep_AnOpenedHiddenEntryIsAMissOnlyUnderAFinding(t *testing.T) {
	scorer := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9}}
	store := laneLedger(t, true)
	world, session := newDeepWorld(t, "deep-opened", sdd.VerifierPolicy{}, proctest.WithEntries(laneEntries()...),
		proctest.WithRelevance(store), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: laneJudge}))
	x := startDeep(t, world, session, captureDraft())
	reading := verifierReading(t, x)
	var hidden []string
	for i := range 40 {
		id := fmt.Sprintf("20260603-1200%02d-d-tac-l%02d", i, i)
		if readingLine(reading, id) == "" {
			hidden = append(hidden, id)
		}
	}
	if len(hidden) < 2 {
		t.Fatalf("the reading should set the low entries aside:\n%s", reading)
	}
	missed, opened := hidden[0], hidden[1]
	x.delegate.LogRead(t, "show", []string{missed, opened}, nil)
	x.delegate.Answer(t, x.child, "read", "judge", map[string]any{"findings": []any{
		map[string]any{"verdict": "the gap restates a hidden directive", "axis": "duplicates", "severity": "low", "basis": []any{missed},
			"edits": []any{deepEdit("observes something.", "observes something the directive does not.")}},
	}}, "")
	x.pickUp(t)
	session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	session.Answer(t, x.instance, "deepProposal", "accept", nil, "")
	session.Answer(t, x.instance, "playback", "confirm", nil, "yes, with the verifier's changes")
	serve := session.Answer(t, x.instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	entryID, _ := serve.Produced["entryId"].(string)

	segments, err := store.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := ledger.Reader(model.NewRelevancePair(entryID, missed)); got.Code != model.ReaderReferenced || !got.Audit {
		t.Fatalf("the hidden basis = %+v: want a referenced audit sample, a miss", got)
	}
	if got, _ := ledger.Reader(model.NewRelevancePair(entryID, opened)); got.Code != model.ReaderImplicit || got.Audit || !got.OpenedHidden {
		t.Fatalf("the hidden opening = %+v: want the drafter's implicit dismissal with the opening flag and no audit bit", got)
	}
	status, err := world.App.GroundingStatus(t.Context(), world.Identity, "proctest", sdd.GroundingStatusRequest{Verbose: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"opened hidden: 1 entries"} {
		if !strings.Contains(status.Text, want) {
			t.Errorf("status lacks %q:\n%s", want, status.Text)
		}
	}
}

// A hidden entry an earlier verifier opened keeps its opening once a fresh
// verifier replaced it, and the capture publishes.
func TestCaptureDeep_AnEndedVerifiersOpeningCountsAtPublish(t *testing.T) {
	scorer := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9}}
	store := laneLedger(t, true)
	launcher := headlessLauncher(t)
	world, session := newDeepWorld(t, "deep-opened-ended", sdd.VerifierPolicy{Runner: "claude"}, proctest.WithEntries(laneEntries()...),
		proctest.WithRelevance(store), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: laneJudge}), proctest.WithDelegateLauncher(launcher))
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	round := launcher.NextRound(t)
	dispatch := &sdd.WorkflowDispatch{Session: round.Launch.Session, Instance: round.Launch.Instance}
	x := &deepExchange{world: world, session: session, instance: instance, dispatch: dispatch, delegate: world.Delegate(t, dispatch), child: round.Launch.Instance}
	reading := verifierReading(t, x)
	opened := ""
	for i := range 40 {
		if id := fmt.Sprintf("20260603-1200%02d-d-tac-l%02d", i, i); readingLine(reading, id) == "" {
			opened = id
			break
		}
	}
	if opened == "" {
		t.Fatalf("the reading should set the low entries aside:\n%s", reading)
	}
	x.delegate.LogRead(t, "show", []string{opened}, nil)
	x.delegate.Answer(t, x.child, "read", "judge", map[string]any{"findings": subjectFinding("observes something.")}, "")
	round.Finish("", nil)
	waitForRounds(t, world, round.Launch.Session, 1)
	proctest.RequireStep(t, x.pickUp(t), "deepAnswer")
	proctest.RequireStep(t, x.settleWithAChange(t, "A tactical gap: the fixture observes a subject twice."), "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	driveRound(t, world, launcher.NextRound(t))
	if _, _, err := world.App.ResumeWorkflow(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: round.Launch.Session}); err == nil {
		t.Fatal("the fresh verifier should end the one it replaced")
	}
	serve = x.pickUp(t)
	if serve.PendingOperation != nil {
		t.Fatalf("the publish should succeed, it failed: %s", serve.PendingOperation.Error)
	}
	proctest.RequireStep(t, serve, "verifySummary")
	serve = session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	entryID, _ := serve.Produced["entryId"].(string)
	segments, err := store.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := ledger.Reader(model.NewRelevancePair(entryID, opened)); !got.OpenedHidden {
		t.Fatalf("the ended verifier's opening = %+v: want the opening flag", got)
	}
}
