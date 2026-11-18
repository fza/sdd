package model_test

import (
	"math"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// The tally counts the labelled pairs whatever judge scored them, by how
// people answered; --exclude-replay leaves the seeded ones out.
func TestRelevanceLedger_TallyCountsLabelsWithoutAJudge(t *testing.T) {
	topicOnly, err := model.VerdictReasonByte(model.VerdictTopicOnly)
	if err != nil {
		t.Fatal(err)
	}
	covered, err := model.VerdictReasonByte(model.VerdictCovered)
	if err != nil {
		t.Fatal(err)
	}
	record := func(a string, verdict model.ReaderVerdict) model.ReaderRecord {
		return model.ReaderRecord{Pair: model.NewRelevancePair(a, "20260601-000000-d-tac-sub"), Verdict: verdict}
	}
	ledger := model.NewRelevanceLedger(model.RelevanceBatch{Readers: []model.ReaderRecord{
		record("20260101-000000-s-tac-ref", model.ReaderVerdict{Code: model.ReaderReferenced}),
		record("20260101-000001-s-tac-rep", model.ReaderVerdict{Code: model.ReaderReferenced, Source: model.SourceReplay}),
		record("20260101-000002-s-tac-imp", model.ReaderVerdict{Code: model.ReaderImplicit}),
		record("20260101-000003-s-tac-flp", model.ReaderVerdict{Code: model.ReaderImplicit, Flipped: true}),
		record("20260101-000004-s-tac-top", model.ReaderVerdict{Code: model.ReaderDismissed, Reason: topicOnly}),
		record("20260101-000005-s-tac-cov", model.ReaderVerdict{Code: model.ReaderDismissed, Reason: covered}),
	}})

	all := ledger.Tally(false)
	if want := (model.LabelTally{Labelled: 5, Referenced: 2, Implicit: 2, Flipped: 1, Coded: 1}); all != want {
		t.Errorf("Tally = %+v, want %+v", all, want)
	}
	live := ledger.Tally(true)
	if want := (model.LabelTally{Labelled: 4, Referenced: 1, Implicit: 2, Flipped: 1, Coded: 1}); live != want {
		t.Errorf("Tally without replay = %+v, want %+v", live, want)
	}
}

// A judge that misses nothing reaches each recall stage from the positive
// weight StagePositives names: about 35, 73 and 188.
func TestStagePositives(t *testing.T) {
	for _, tc := range []struct {
		stage float64
		held  float64
	}{{0.90, 35}, {0.95, 73}, {0.98, 189}} {
		if got := model.StagePositives(tc.stage); math.Ceil(got) != tc.held {
			t.Errorf("StagePositives(%.2f) = %v, want just below %v", tc.stage, got, tc.held)
		}
		if model.WilsonLowerWeighted(tc.held-1, tc.held-1) >= tc.stage || model.WilsonLowerWeighted(tc.held, tc.held) < tc.stage {
			t.Errorf("stage %.2f should hold from the %vth missless positive on", tc.stage, tc.held)
		}
	}
	if got := model.RecallStages(); len(got) != 3 || got[0] != 0.90 || got[1] != 0.95 || got[2] != model.RecallFloor {
		t.Fatalf("RecallStages = %v, want 0.90, 0.95 and the floor", got)
	}
}

// The report names the next stage and the positive weight it still needs;
// the top stage has no next.
func TestCalibrationReport_NextStage(t *testing.T) {
	for _, tc := range []struct {
		positives float64
		next      float64
		needed    float64
	}{{0, 0.90, 35}, {35, 0.95, 38}, {80, 0.98, 109}, {189, 0, 0}} {
		report := model.CalibrationReport{PositiveWeight: tc.positives}
		next, needed := report.NextStage()
		if next != tc.next || math.Ceil(needed) != tc.needed {
			t.Errorf("%v positives: next stage %.2f needing %v, want %.2f needing %v", tc.positives, next, needed, tc.next, tc.needed)
		}
	}
}
