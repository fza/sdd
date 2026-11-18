package model_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// calibrationLedger holds, for each subject, positives referenced pairs and
// negatives implicit dismissals; scored names candidates judge "j" scored.
func calibrationLedger(subjects map[string][2]int, scored ...string) *model.RelevanceLedger {
	var batch model.RelevanceBatch
	for subject, counts := range subjects {
		for i := range counts[0] + counts[1] {
			candidate := fmt.Sprintf("20260101-%06d-s-tac-%s", i, subject[len(subject)-3:])
			code := model.ReaderReferenced
			if i >= counts[0] {
				code = model.ReaderImplicit
			}
			pair := model.NewRelevancePair(subject, candidate)
			batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: code}})
			if slices.Contains(scored, candidate) {
				batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: "j", Score: 100}})
			}
		}
	}
	return model.NewRelevanceLedger(batch)
}

// Each subject with an unscored positive goes once: its unscored positives
// and a stable sample of its other unscored labelled candidates up to the
// size, the subjects with the most positive weight first.
func TestCalibrationBatches_PositivesWithASampleOfTheirSubject(t *testing.T) {
	const (
		few   = "20260601-000000-d-tac-few"
		many  = "20260602-000000-d-tac-mny"
		none  = "20260603-000000-d-tac-non"
		small = "20260604-000000-d-tac-sml"
	)
	ledger := calibrationLedger(map[string][2]int{few: {1, 100}, many: {3, 100}, none: {0, 50}, small: {1, 4}}, "20260101-000001-s-tac-few")

	batches := ledger.CalibrationBatches("j", 60)
	if len(batches) != 3 {
		t.Fatalf("%d batches %+v: only the subjects holding an unscored positive go", len(batches), batches)
	}
	if batches[0].Subject != many || batches[0].Positive != 3 {
		t.Fatalf("first batch %+v: the subject with the most positive weight goes first", batches[0])
	}
	if batches[1].Subject != small || batches[2].Subject != few {
		t.Fatalf("batches %v, %v: equal weights go latest first", batches[1].Subject, batches[2].Subject)
	}
	for _, batch := range batches {
		seen := map[string]bool{}
		for _, id := range batch.Candidates {
			if seen[id] {
				t.Fatalf("batch %s sends %s twice", batch.Subject, id)
			}
			seen[id] = true
			if id == "20260101-000001-s-tac-few" {
				t.Fatalf("batch %s sends a candidate judge j already scored", batch.Subject)
			}
		}
	}
	if got := batches[0].Candidates; len(got) != 60 || !slices.Contains(got, "20260101-000000-s-tac-mny") || !slices.Contains(got, "20260101-000002-s-tac-mny") {
		t.Fatalf("batch of %d %v: every positive goes with a sample filling the size", len(got), got)
	}
	if got := batches[1].Candidates; len(got) != 5 {
		t.Fatalf("batch %v: a subject with fewer labelled candidates sends them all", got)
	}
	again := ledger.CalibrationBatches("j", 60)
	if !slices.Equal(again[0].Candidates, batches[0].Candidates) {
		t.Fatal("the sample should be the same on every plan")
	}
}
