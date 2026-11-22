package model_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func orderedIDs(candidates []model.GatherCandidate) []string {
	ids := make([]string, len(candidates))
	for i, candidate := range candidates {
		ids[i] = candidate.Entry.ID
	}
	return ids
}

// The searches' hits come first by reciprocal rank fusion over their
// ranks, then the entries a topic listing alone found by similarity to the
// draft, those without one last.
func TestOrderGatherCandidates_FusesSearchRanksThenSimilarity(t *testing.T) {
	gap := func(id string) *model.Entry {
		return &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindGap, Summary: id}
	}
	first, second, third := gap("20260101-100000-s-tac-fst"), gap("20260101-100001-s-tac-snd"), gap("20260101-100002-s-tac-thd")
	near, far, unknown := gap("20260101-100003-s-tac-nea"), gap("20260101-100004-s-tac-far"), gap("20260101-100005-s-tac-unk")
	g := model.NewGraph([]*model.Entry{first, second, third, near, far, unknown})
	reads := []model.GroundingRead{
		{Mode: model.GroundingKeyword, Text: "t", IDs: []string{second.ID, third.ID}},
		{Mode: model.GroundingSemantic, IDs: []string{first.ID, second.ID}},
		{Mode: model.GroundingListing, Text: "a/b", IDs: []string{unknown.ID, far.ID, near.ID, first.ID}},
	}
	candidates, _ := model.GatherCandidates(g, model.GroundingPopulation(g), reads, nil, nil)
	similarity := map[string]float64{near.ID: 0.9, far.ID: 0.2}

	got := orderedIDs(model.OrderGatherCandidates(g, candidates, reads, similarity, nil))
	want := []string{second.ID, first.ID, third.ID, near.ID, far.ID, unknown.ID}
	if !slices.Equal(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
	if unsearched := model.Unsearched(candidates, reads); !slices.Equal(unsearched, []string{unknown.ID, far.ID, near.ID}) {
		t.Errorf("unsearched %v: the entries a listing alone found", unsearched)
	}
}

// On a tie an entry one reference hop from the draft's references comes
// first and a done entry last; a link between candidates or an entry's date
// moves nothing else.
func TestOrderGatherCandidates_BreaksTiesByReferenceHopAndDone(t *testing.T) {
	reference := &model.Entry{ID: "20260101-090000-d-tac-ref", Type: model.TypeDecision, Kind: model.KindDirective, Summary: "Referenced."}
	plain := &model.Entry{ID: "20260101-100000-s-tac-pln", Type: model.TypeSignal, Kind: model.KindGap, Summary: "Plain."}
	done := &model.Entry{ID: "20260101-100001-s-tac-don", Type: model.TypeSignal, Kind: model.KindDone, Summary: "Done."}
	hop := &model.Entry{ID: "20260101-100002-s-tac-hop", Type: model.TypeSignal, Kind: model.KindGap, Summary: "Next to the reference.",
		Refs: []model.Ref{{ID: reference.ID, Kind: model.RefKindRelated}}}
	newer := &model.Entry{ID: "20260301-100000-d-tac-nwr", Type: model.TypeDecision, Kind: model.KindDirective, Summary: "Refines the plain gap.",
		Refs: []model.Ref{{ID: plain.ID, Kind: model.RefKindRefines}}}
	g := model.NewGraph([]*model.Entry{reference, plain, done, hop, newer})
	reads := []model.GroundingRead{
		{Mode: model.GroundingKeyword, Text: "a", IDs: []string{done.ID}},
		{Mode: model.GroundingKeyword, Text: "b", IDs: []string{plain.ID}},
		{Mode: model.GroundingKeyword, Text: "c", IDs: []string{hop.ID}},
		{Mode: model.GroundingKeyword, Text: "d", IDs: []string{newer.ID}},
	}
	candidates, _ := model.GatherCandidates(g, model.GroundingPopulation(g), reads, nil, nil)

	got := orderedIDs(model.OrderGatherCandidates(g, candidates, reads, nil, []string{reference.ID}))
	want := []string{hop.ID, plain.ID, newer.ID, done.ID}
	if !slices.Equal(got, want) {
		t.Fatalf("order %v, want %v: the reference hop first, the done entry last, the rest as the reads found them", got, want)
	}
}
