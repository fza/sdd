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

// Every entry the reading showed ends in one state: referenced, dismissed
// with its code, opened when served in full, else dismissed implicitly; an
// entry only listed as a set-aside gets no record.
func TestCaptureRelevance_GivesEveryShownEntryOneState(t *testing.T) {
	const (
		entry     = "20261110-120000-d-tac-new"
		ref       = "20261101-120000-s-tac-ref"
		dismissed = "20261102-120000-s-tac-dis"
		opened    = "20261103-120000-s-tac-opn"
		left      = "20261104-120000-s-tac-lft"
		sample    = "20261105-120000-s-tac-aud"
		aside     = "20261106-120000-s-tac-asd"
	)
	reading := &model.Reading{}
	reading.Fold(model.ReadPayload{
		Reads:     []model.GroundingRead{{Mode: model.GroundingBriefs, IDs: []string{ref, dismissed, opened, left, sample, aside}}},
		Full:      []string{opened},
		Shown:     []string{ref, dismissed, left, sample, entry},
		Prefilter: &model.PrefilterPayload{Judge: "j", Audit: []string{sample}},
	})
	batch, err := model.CaptureRelevance(entry, reading, []string{ref}, []model.GroundingVerdict{{IDs: []string{dismissed}, Code: model.VerdictTopicOnly}})
	if err != nil {
		t.Fatal(err)
	}
	topicOnly, _ := model.VerdictReasonByte(model.VerdictTopicOnly)
	want := []model.ReaderRecord{
		{Pair: model.NewRelevancePair(entry, ref), Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}},
		{Pair: model.NewRelevancePair(entry, dismissed), Verdict: model.ReaderVerdict{Code: model.ReaderDismissed, Reason: topicOnly}},
		{Pair: model.NewRelevancePair(entry, opened), Verdict: model.ReaderVerdict{Code: model.ReaderOpened}},
		{Pair: model.NewRelevancePair(entry, left), Verdict: model.ReaderVerdict{Code: model.ReaderImplicit}},
		{Pair: model.NewRelevancePair(entry, sample), Verdict: model.ReaderVerdict{Code: model.ReaderImplicit, Audit: true}},
	}
	if !slices.Equal(batch.Readers, want) {
		t.Fatalf("records %+v\nwant %+v", batch.Readers, want)
	}
}

func TestRefPairs_PairsEachEntryWithWhatItAnswers(t *testing.T) {
	const (
		old    = "20260101-000000-d-tac-old"
		gap    = "20260101-000001-s-tac-gap"
		actor  = "20260101-000002-d-ops-act"
		newer  = "20260102-000000-d-tac-new"
		closer = "20260103-000000-s-tac-don"
	)
	g := model.NewGraph([]*model.Entry{
		{ID: old, Type: model.TypeDecision, Kind: model.KindDirective},
		{ID: gap, Type: model.TypeSignal, Kind: model.KindGap},
		{ID: actor, Type: model.TypeDecision, Kind: model.KindActor},
		{ID: newer, Type: model.TypeDecision, Kind: model.KindDirective, Refs: []model.Ref{{ID: gap}, {ID: actor}, {ID: "20250101-000000-s-tac-gon"}}, Supersedes: []string{old}},
		{ID: closer, Type: model.TypeSignal, Kind: model.KindDone, Refs: []model.Ref{{ID: gap}}, Closes: []string{newer, gap}},
		{ID: "20260104-000000-s-prc-bas", Type: model.TypeSignal, Kind: model.KindInsight, Embedded: true, Refs: []model.Ref{{ID: gap}}},
		{ID: "20260105-000000-s-tac-emb", Type: model.TypeSignal, Kind: model.KindGap, Refs: []model.Ref{{ID: "20260104-000000-s-prc-bas"}}},
	})
	want := []model.RelevancePair{model.NewRelevancePair(old, newer), model.NewRelevancePair(gap, newer), model.NewRelevancePair(gap, closer), model.NewRelevancePair(newer, closer)}
	if got := model.RefPairs(g); !slices.Equal(got, want) {
		t.Fatalf("pairs %v, want %v: each answered entry the graph holds once, no actor and no entry the binary ships", got, want)
	}
}
