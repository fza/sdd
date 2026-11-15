package model_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// The shown set holds what a reading printed to judge and what it served in
// full; an entry a read listed only as a set-aside ID stays out.
func TestReading_ShownHoldsBriefsAndFullReadsButNotSetAsides(t *testing.T) {
	reading := &model.Reading{}
	reading.Fold(model.ReadPayload{
		Reads: []model.GroundingRead{{Mode: model.GroundingBriefs, IDs: []string{"brief", "full", "aside"}}},
		Full:  []string{"full"},
		Shown: []string{"brief"},
	})
	reading.Fold(model.ReadPayload{Reads: []model.GroundingRead{{Mode: model.GroundingLate, IDs: []string{"late"}}}, Shown: []string{"late"}})
	if got := reading.Shown(); !slices.Equal(got, []string{"brief", "full", "late"}) {
		t.Fatalf("shown %v: briefs, full reads and late briefs should be shown, set-asides not", got)
	}
	if !reading.IsListed("aside") {
		t.Fatal("a set-aside should stay listed")
	}
}

// A payload logged before the shown record existed decodes and folds, its
// shown set holding the full reads alone.
func TestReading_PayloadWithoutShownStillFolds(t *testing.T) {
	var payload model.ReadPayload
	if err := json.Unmarshal([]byte(`{"reads":[{"mode":"briefs","ids":["a","b"]}],"full":["a"]}`), &payload); err != nil {
		t.Fatal(err)
	}
	reading := &model.Reading{}
	reading.Fold(payload)
	if got := reading.Shown(); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("shown %v: an old payload should show its full reads alone", got)
	}
}
