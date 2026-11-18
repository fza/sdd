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

// A judge that misses nothing reaches the recall floor from about 188
// referenced pairs.
func TestRecallFloorPositives(t *testing.T) {
	if got := model.RecallFloorPositives(); math.Round(got) != 188 {
		t.Fatalf("RecallFloorPositives = %v, want about 188", got)
	}
	if model.WilsonLowerWeighted(188, 188) >= model.RecallFloor || model.WilsonLowerWeighted(189, 189) < model.RecallFloor {
		t.Fatal("the floor should hold from the 189th missless positive on")
	}
}
