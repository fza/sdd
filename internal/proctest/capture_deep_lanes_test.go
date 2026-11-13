package proctest_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/local"
	"github.com/networkteam/sdd/test"
)

const (
	laneJudge = "test:scripted"
	// laneRelevant scores between the set-aside and the relevant threshold;
	// every other fixture entry scores below both.
	laneRelevant = "20260602-120000-d-tac-hig"
)

// laneEntries are forty directives on a topic the draft does not carry,
// so only their scores decide their lane, and laneRelevant.
func laneEntries() []*model.Entry {
	entries := []*model.Entry{{
		ID: laneRelevant, Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Intent: model.IntentPending,
		Summary: "A directive the scorer rates relevant.", Content: "A directive the scorer rates relevant.", Topics: proctest.MustTopics("misc"),
	}}
	for i := range 40 {
		entries = append(entries, &model.Entry{
			ID: fmt.Sprintf("20260603-1200%02d-d-tac-l%02d", i, i), Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Intent: model.IntentPending,
			Summary: fmt.Sprintf("Low directive %d.", i), Content: fmt.Sprintf("Low directive %d.", i), Topics: proctest.MustTopics("misc"),
		})
	}
	return entries
}

// laneLedger holds labels that put the judge's recall bound at the floor,
// with the set-aside threshold at a score of 200 of 255 and the relevant
// one at 250, where the referenced share first holds.
func laneLedger(t *testing.T, calibrated bool) *local.MemoryRelevanceStore {
	t.Helper()
	store := local.NewMemoryRelevanceStore()
	if !calibrated {
		return store
	}
	var batch model.RelevanceBatch
	for i := range 189 {
		pair := model.NewRelevancePair(fmt.Sprintf("20250101-%06d-s-tac-pos", i), "20250601-000000-d-tac-sub")
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}})
		batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: laneJudge, Score: 200}})
	}
	for i := range 20 {
		pair := model.NewRelevancePair(fmt.Sprintf("20250102-%06d-s-tac-top", i), "20250601-000000-d-tac-sub")
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}})
		batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: laneJudge, Score: 250}})
	}
	for i := range 200 {
		pair := model.NewRelevancePair(fmt.Sprintf("20250103-%06d-s-tac-neg", i), "20250601-000000-d-tac-sub")
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderDismissed, Reason: 1}})
		batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: laneJudge, Score: 200}})
	}
	if err := store.Append(t.Context(), relevance.Encode(batch)); err != nil {
		t.Fatal(err)
	}
	return store
}

// verifierReading drives a capture to the verify delegate and returns the
// whole reading it serves.
func verifierReading(t *testing.T, x *deepExchange) string {
	t.Helper()
	_, resume := x.world.Resume(t, x.dispatch.Session, "verifier")
	var reading strings.Builder
	reading.WriteString(resume.Open[0].Instructions)
	x.delegate.Refresh(t)
	count := readingPages(t, resume.Open[0].Instructions)
	for range count - 1 {
		serve := x.delegate.Answer(t, x.child, "read", "more", nil, "")
		reading.WriteString(serve.Instructions)
	}
	return reading.String()
}

// Once the prefilter's calibration holds, the verifier inspects the
// structural entries and those scoring at or above the threshold as blocks
// and gets every other listed entry as one line, the audit share back as
// blocks (20261109-004506-d-cpt-udz, D29, D33, D36). Each pair is scored
// once: publish writes the scores to the ledger.
func TestCaptureDeep_ACalibratedReadingHidesWhatScoresBelowTheThreshold(t *testing.T) {
	scorer := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9}}
	store := laneLedger(t, true)
	world, session := newDeepWorld(t, "deep-lanes", sdd.VerifierPolicy{}, proctest.WithEntries(laneEntries()...),
		proctest.WithRelevance(store), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: laneJudge}))
	x := startDeep(t, world, session, captureDraft())
	reading := verifierReading(t, x)

	for _, want := range []string{"ID: " + captureRefID, "ID: " + laneRelevant} {
		if !strings.Contains(reading, want) {
			t.Errorf("the declared target and the relevant entry should be served as blocks, lacking %q:\n%s", want, reading)
		}
	}
	if !strings.Contains(reading, "## Listed below the prefilter's threshold (") {
		t.Fatalf("the reading should list the hidden entries in a compact block with their count:\n%s", reading)
	}
	var hidden, audited []string
	for i := range 40 {
		id := fmt.Sprintf("20260603-1200%02d-d-tac-l%02d", i, i)
		switch {
		case strings.Contains(reading, "\n"+id+"  Low directive"):
			hidden = append(hidden, id)
		case strings.Contains(reading, "ID: "+id):
			audited = append(audited, id)
		default:
			t.Fatalf("listed entry %s should be served, as a line or a block:\n%s", id, reading)
		}
	}
	if len(audited) == 0 || len(audited) > 3 || len(hidden)+len(audited) != 40 {
		t.Fatalf("%d hidden and %d audited: the audit share should return blind as blocks", len(hidden), len(audited))
	}
	if slices.Contains(scorer.Scored(), captureRefID) {
		t.Fatal("a structural entry should not be scored: its lane is settled")
	}
	paid := len(scorer.Scored())

	serve := x.delegate.Answer(t, x.child, "read", "judge", map[string]any{"findings": []any{}}, "")
	proctest.RequireStep(t, serve, "waitAnswers")
	proctest.RequireStep(t, x.pickUp(t), "verifySummary")
	serve = session.Answer(t, x.instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	entryID, _ := serve.Produced["entryId"].(string)
	if len(scorer.Scored()) != paid {
		t.Fatalf("the scorer was asked %d more times after the reading: each pair should be paid once", len(scorer.Scored())-paid)
	}
	segments, err := store.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ledger.Score(model.NewRelevancePair(entryID, hidden[0]), entryID, laneJudge); !ok {
		t.Fatal("publish should write the verifier's fresh scores to the ledger")
	}
}

// Until the calibration holds the verifier's reading is served as before:
// every listed entry as a block, and no scorer call.
func TestCaptureDeep_AnUncalibratedReadingServesEveryEntryAsABlock(t *testing.T) {
	scorer := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9}}
	world, session := newDeepWorld(t, "deep-lanes-off", sdd.VerifierPolicy{}, proctest.WithEntries(laneEntries()...),
		proctest.WithRelevance(laneLedger(t, false)), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: laneJudge}))
	x := startDeep(t, world, session, captureDraft())
	reading := verifierReading(t, x)
	if strings.Contains(reading, "## Listed below the prefilter's threshold") || !strings.Contains(reading, "ID: 20260603-120000-d-tac-l00") {
		t.Fatalf("an uncalibrated reading should hide nothing:\n%s", reading)
	}
	if len(scorer.Scored()) != 0 {
		t.Fatal("an uncalibrated reading should pay for no score")
	}
}

// Scores gather's prefilter left in the reading split the verifier's lanes
// without a second call.
func TestCaptureDeep_TheReadingsScoresAreNotPaidAgain(t *testing.T) {
	scorer := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9}}
	world, session := newDeepWorld(t, "deep-lanes-reuse", sdd.VerifierPolicy{}, proctest.WithEntries(laneEntries()...),
		proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1}),
		proctest.WithRelevance(laneLedger(t, true)), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: laneJudge}))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Report(t, instance, map[string]any{"body": deepBody, "entryKind": "gap", "topics": []any{"cli/ux"}})
	session.Gather(t, instance)
	paid := len(scorer.Scored())
	if paid == 0 {
		t.Fatal("gather's prefilter should score the candidates")
	}
	draft := captureDraft()
	draft["verdicts"] = []any{map[string]any{"ids": []any{laneRelevant}, "code": "topic-only"}}
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, instance, "playback", "confirm", nil, "write it")
	proctest.RequireStep(t, serve, "deepDispatch")
	x := dispatchDeep(t, world, session, instance)
	reading := verifierReading(t, x)
	if !strings.Contains(reading, "## Listed below the prefilter's threshold (") {
		t.Fatalf("the reading's scores should hide the low entries:\n%s", reading)
	}
	if extra := scorer.Scored()[paid:]; len(extra) != 0 {
		t.Fatalf("the verifier scored %v again: a pair the reading holds should not be paid twice", extra)
	}
}
