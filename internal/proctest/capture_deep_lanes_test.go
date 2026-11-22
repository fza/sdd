package proctest_test

import (
	"fmt"
	"slices"
	"strconv"
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

// readingLine returns the line of the verifier's reading that lists id,
// empty when none does.
func readingLine(reading, id string) string {
	for line := range strings.Lines(reading) {
		marks, rest, ok := strings.Cut(line, "  "+id)
		if ok && len(marks) == 10 && (rest == "\n" || strings.HasPrefix(rest, " ")) {
			return strings.TrimSuffix(line, "\n")
		}
	}
	return ""
}

// setAsideCount returns the count the reading gives reason, 0 when it names
// none.
func setAsideCount(reading, reason string) int {
	for line := range strings.Lines(reading) {
		count, found, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && strings.HasPrefix(found, reason) {
			n, err := strconv.Atoi(count)
			if err == nil {
				return n
			}
		}
	}
	return 0
}

// Once the prefilter's calibration holds, the verifier gets the declared
// target in full and every other entry as a line: the one between the
// thresholds in doubt, the ones below the set-aside threshold counted, the
// audit share back among the lines, unscored. Each pair is scored once:
// publish writes the scores to the ledger.
func TestCaptureDeep_ACalibratedReadingSetsAsideWhatScoresBelowTheThreshold(t *testing.T) {
	scorer := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9}}
	store := laneLedger(t, true)
	world, session := newDeepWorld(t, "deep-lanes", sdd.VerifierPolicy{}, proctest.WithEntries(laneEntries()...),
		proctest.WithRelevance(store), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: laneJudge}))
	x := startDeep(t, world, session, captureDraft())
	reading := verifierReading(t, x)

	if !strings.Contains(reading, "ID: "+captureRefID) || strings.Contains(reading, "ID: "+laneRelevant) {
		t.Fatalf("only the declared target should be served in full:\n%s", reading)
	}
	if line := readingLine(reading, laneRelevant); !strings.HasPrefix(line, "  .90") {
		t.Fatalf("the entry between the thresholds should be a line in doubt with its score, got %q:\n%s", line, reading)
	}
	var audited []string
	for i := range 40 {
		id := fmt.Sprintf("20260603-1200%02d-d-tac-l%02d", i, i)
		if line := readingLine(reading, id); line != "" {
			if !strings.HasPrefix(line, "  ?") {
				t.Fatalf("an audit sample should come as an unscored line, got %q", line)
			}
			audited = append(audited, id)
		}
	}
	setAside := setAsideCount(reading, model.SetAsideFiltered)
	if len(audited) == 0 || len(audited) > 3 || setAside < 40-len(audited) {
		t.Fatalf("%d set aside and %d audited: the low entries should be counted but for the audit share:\n%s", setAside, len(audited), reading)
	}
	if strings.Contains(reading, "20260603-120000-d-tac-l00") && !slices.Contains(audited, "20260603-120000-d-tac-l00") {
		t.Fatalf("a set-aside entry should be counted without its ID:\n%s", reading)
	}
	if slices.Contains(scorer.Scored(), captureRefID) {
		t.Fatal("a declared target should not be scored: it is served in full")
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
	if _, ok := ledger.Score(model.NewRelevancePair(entryID, "20260603-120000-d-tac-l00"), entryID, laneJudge); !ok {
		t.Fatal("publish should write the verifier's fresh scores to the ledger")
	}
}

// Until the calibration holds the judge still ranks the reading: its best
// scores at 0.5 or more are marked lines, done entries too, and the rest
// lines in doubt; nothing is served in full but the target.
func TestCaptureDeep_AnUncalibratedReadingMarksTheJudgesBest(t *testing.T) {
	const done = "20260602-130000-s-tac-don"
	scorer := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9, done: 0.95}}
	entries := append(laneEntries(), &model.Entry{
		ID: done, Type: model.TypeSignal, Kind: model.KindDone, Layer: model.LayerTactical,
		Summary: "A done the scorer rates highest.", Content: "A done the scorer rates highest.", Topics: proctest.MustTopics("misc"),
	})
	world, session := newDeepWorld(t, "deep-lanes-off", sdd.VerifierPolicy{}, proctest.WithEntries(entries...),
		proctest.WithRelevance(laneLedger(t, false)), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: laneJudge}))
	x := startDeep(t, world, session, captureDraft())
	reading := verifierReading(t, x)
	if len(scorer.Scored()) == 0 || strings.Contains(reading, "ID: "+laneRelevant) || strings.Contains(reading, "ID: "+done) {
		t.Fatalf("the judge should rank the reading and serve nothing in full but the target:\n%s", reading)
	}
	if !strings.Contains(reading, "## marked for you (2)") || !strings.HasPrefix(readingLine(reading, done), "! .95") || !strings.HasPrefix(readingLine(reading, laneRelevant), "! .90") {
		t.Fatalf("the two scores at 0.5 or more should be marked lines:\n%s", reading)
	}
	if !strings.Contains(readingLine(reading, laneRelevant), " — "+model.VerifierLikelyBearing) {
		t.Fatalf("a marked line should name why sdd marked it and what the drafter did:\n%s", reading)
	}
	for i := range 40 {
		id := fmt.Sprintf("20260603-1200%02d-d-tac-l%02d", i, i)
		if line := readingLine(reading, id); !strings.HasPrefix(line, "  .00") {
			t.Fatalf("listed entry %s should be a line in doubt with its score, got %q:\n%s", id, line, reading)
		}
	}
	if strings.Contains(reading, "## set aside (") {
		t.Fatalf("an uncalibrated reading should set nothing aside:\n%s", reading)
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
	if setAsideCount(reading, model.SetAsideFiltered) == 0 {
		t.Fatalf("the reading's scores should set the low entries aside:\n%s", reading)
	}
	if extra := scorer.Scored()[paid:]; len(extra) != 0 {
		t.Fatalf("the verifier scored %v again: a pair the reading holds should not be paid twice", extra)
	}
}

// The drafter's findings list states which of the lines sdd marked for the
// verifier it opened, as its delegate's reads report.
func TestCaptureDeep_TheFindingsNameTheMarkedLinesTheVerifierOpened(t *testing.T) {
	const done = "20260602-130000-s-tac-don"
	scorer := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9, done: 0.95}}
	entries := append(laneEntries(), &model.Entry{
		ID: done, Type: model.TypeSignal, Kind: model.KindDone, Layer: model.LayerTactical,
		Summary: "A done the scorer rates highest.", Content: "A done the scorer rates highest.", Topics: proctest.MustTopics("misc"),
	})
	world, session := newDeepWorld(t, "deep-marked-opened", sdd.VerifierPolicy{}, proctest.WithEntries(entries...),
		proctest.WithRelevance(laneLedger(t, false)), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: laneJudge}))
	x := startDeep(t, world, session, captureDraft())
	reading := verifierReading(t, x)
	if !strings.HasPrefix(readingLine(reading, laneRelevant), "!") || !strings.HasPrefix(readingLine(reading, done), "!") {
		t.Fatalf("both high scores should be marked lines:\n%s", reading)
	}
	x.delegate.LogRead(t, "show", []string{laneRelevant}, nil)
	x.delegate.Answer(t, x.child, "read", "judge", map[string]any{"findings": subjectFinding("observes something.")}, "")
	serve := x.pickUp(t)
	proctest.RequireStep(t, serve, "deepAnswer")
	if want := "The verifier opened 1 of the 2 lines sdd marked for it: " + laneRelevant + "; not opened: " + done + "."; !strings.Contains(serve.Instructions, want) {
		t.Fatalf("the findings should name which marked lines the verifier opened:\n%s", serve.Instructions)
	}
}
