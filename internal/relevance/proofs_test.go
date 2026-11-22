package relevance_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
	"github.com/networkteam/sdd/internal/segment"
)

func proofFixture() model.ProofRecords {
	at := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	return model.ProofRecords{
		Replays: []model.ReplayCheck{{Subject: "s", Judge: "chat:m", Revision: "r1", At: at, GraphSize: 250, Planted: "a", Caught: true,
			Candidates: []model.ReplayCandidate{{ID: "a", Score: 200, Far: true, Lowest: 10, Median: 40, Needed: true}, {ID: "b", Score: 3, Spared: true}}}},
		Tallies: []model.CheckTally{{Capture: "c", Judge: "chat:m", At: at, Rules: []model.RuleTally{{Rule: model.RuleFarFloor, Step: model.StepNarrowed, Hidden: []string{"h"}, Would: 4}}, Margins: []float64{0.25, -0.01}}},
		Scans:   []model.Scanned{{Capture: "c", At: at}},
		Misses:  []model.Miss{{Capture: "c", Hidden: "h", Rule: model.RuleFarFloor, Detector: model.DetectScan, Judge: "chat:m", At: at}},
	}
}

func TestProofs_RoundTripAndFoldDuplicatesOnce(t *testing.T) {
	records := proofFixture()
	data, err := relevance.EncodeProofs(records)
	if err != nil {
		t.Fatal(err)
	}
	folded, err := relevance.FoldProofs([][]byte{data, data})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(folded, records) {
		t.Fatalf("folded %+v: two copies of one segment should fold to its records once", folded)
	}
}

func TestFoldProofs_SkipsUnknownRecordTypesAndRefusesOtherKinds(t *testing.T) {
	data := segment.Encode(relevance.ProofsKind, [][]byte{[]byte(`x{"later":true}`)})
	folded, err := relevance.FoldProofs([][]byte{data})
	if err != nil || len(folded.Replays)+len(folded.Tallies)+len(folded.Scans)+len(folded.Misses) != 0 {
		t.Fatalf("folded %+v, %v: a record type this build does not know should be skipped", folded, err)
	}
	ledger := relevance.Encode(model.RelevanceBatch{})
	if _, err := relevance.FoldProofs([][]byte{ledger}); !errors.Is(err, segment.ErrCorrupt) {
		t.Fatalf("err %v: a ledger segment is no proof segment", err)
	}
	broken := segment.Encode(relevance.ProofsKind, [][]byte{[]byte("p{")})
	if _, err := relevance.FoldProofs([][]byte{broken}); !errors.Is(err, segment.ErrCorrupt) {
		t.Fatalf("err %v: a record that does not decode is corrupt", err)
	}
}
