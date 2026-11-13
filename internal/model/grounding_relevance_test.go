package model_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestVerifierRelevance_ReferencesEachFindingsBasisOnce(t *testing.T) {
	const (
		entry   = "20261110-120000-d-tac-new"
		basis   = "20261101-120000-s-tac-bas"
		sampled = "20261102-120000-s-tac-aud"
	)
	reading := &model.Reading{}
	reading.Fold(model.ReadPayload{Prefilter: &model.PrefilterPayload{Judge: "j", Audit: []string{sampled}}})
	findings := []model.DeepFinding{
		{Verdict: "contradicts the basis", Basis: []string{basis, entry}},
		{Verdict: "rests on the same", Basis: []string{basis, sampled}},
		{Verdict: "no basis"},
	}
	got := model.VerifierRelevance(entry, reading, findings, nil)
	want := []model.ReaderRecord{
		{Pair: model.NewRelevancePair(entry, basis), Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}},
		{Pair: model.NewRelevancePair(entry, sampled), Verdict: model.ReaderVerdict{Code: model.ReaderReferenced, Audit: true}},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("records %+v\nwant %+v: each basis once, live and referenced, the entry itself left out", got, want)
	}
}

// A hidden entry the verifier opened is an audit miss only when a finding
// rests on it; an opening alone carries its own flag and no reader code,
// so it neither labels the pair nor moves the drift rule.
func TestVerifierRelevance_AnOpenedHiddenEntryIsAMissOnlyUnderAFinding(t *testing.T) {
	const (
		entry   = "20261110-120000-d-tac-new"
		missed  = "20261101-120000-s-tac-mis"
		opened  = "20261102-120000-s-tac-opn"
		plainly = "20261103-120000-s-tac-pln"
	)
	findings := []model.DeepFinding{{Verdict: "rests on the hidden one", Basis: []string{missed, plainly}}}
	got := model.VerifierRelevance(entry, &model.Reading{}, findings, []string{missed, opened})
	want := []model.ReaderRecord{
		{Pair: model.NewRelevancePair(entry, missed), Verdict: model.ReaderVerdict{Code: model.ReaderReferenced, Audit: true}},
		{Pair: model.NewRelevancePair(entry, plainly), Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}},
		{Pair: model.NewRelevancePair(entry, opened), Verdict: model.ReaderVerdict{OpenedHidden: true}},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("records %+v\nwant %+v", got, want)
	}
}
