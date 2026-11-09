package model_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestCorpusMarkSinceNamesEveryEntryTheMarkLacks(t *testing.T) {
	mark := model.NewCorpusMark([]string{"20260102-a", "20260101-b", "20260101-b"})
	if got := mark.Since([]string{"20260101-b", "20260102-a"}); len(got) != 0 {
		t.Fatalf("an unchanged population should name nothing new: %v", got)
	}
	// An older ID arriving by merge counts as new, like a later one.
	got := mark.Since([]string{"20251231-old", "20260101-b", "20260102-a", "20260103-new"})
	if !slices.Equal(got, []string{"20251231-old", "20260103-new"}) {
		t.Fatalf("since = %v, want the merged older ID and the later one", got)
	}
	// One entry retired while another arrived: the count alone is unchanged.
	got = mark.Since([]string{"20260101-b", "20260103-new"})
	if !slices.Equal(got, []string{"20260103-new"}) {
		t.Fatalf("since = %v, want the arrival behind an unchanged count", got)
	}
}

func TestGatherCandidatesFoldReadsAndSetAside(t *testing.T) {
	live := &model.Entry{ID: "20260101-100000-s-tac-liv", Type: model.TypeSignal, Kind: model.KindGap, Summary: "Live."}
	twice := &model.Entry{ID: "20260101-100001-s-tac-two", Type: model.TypeSignal, Kind: model.KindGap, Summary: "Found twice."}
	answered := &model.Entry{ID: "20260101-100002-s-tac-ans", Type: model.TypeSignal, Kind: model.KindGap, Summary: "Answered."}
	actor := &model.Entry{ID: "20260101-100003-s-prc-act", Type: model.TypeSignal, Kind: model.KindActor, Summary: "Actor."}
	old := &model.Entry{ID: "20260101-100004-d-tac-old", Type: model.TypeDecision, Kind: model.KindDirective, Summary: "Old."}
	successor := &model.Entry{ID: "20260101-100005-d-tac-new", Type: model.TypeDecision, Kind: model.KindDirective, Summary: "New.", Supersedes: []string{old.ID}}
	g := model.NewGraph([]*model.Entry{live, twice, answered, actor, old, successor})
	population := model.GroundingPopulation(g)
	for _, e := range population {
		if e.ID == actor.ID || e.ID == old.ID {
			t.Fatalf("the population holds %s, an exempt or superseded entry", e.ID)
		}
	}

	reads := []model.GroundingRead{
		{Mode: model.GroundingSemantic, Text: "q", IDs: []string{live.ID, twice.ID, actor.ID, old.ID}},
		{Mode: model.GroundingKeyword, Text: "t", IDs: []string{twice.ID, answered.ID}},
		{Mode: model.GroundingOpened, IDs: []string{successor.ID}},
	}
	candidates, setAside := model.GatherCandidates(g, population, reads, []string{successor.ID}, []string{answered.ID})
	var ids []string
	for _, c := range candidates {
		ids = append(ids, c.Entry.ID)
	}
	if !slices.Equal(ids, []string{twice.ID, live.ID}) {
		t.Fatalf("candidates = %v, want the entry two reads found first", ids)
	}
	if !slices.Equal(candidates[0].FoundBy, []string{"semantic", "keyword(t)"}) {
		t.Errorf("found-by tags = %v", candidates[0].FoundBy)
	}
	reasons := map[string][]string{}
	for _, group := range setAside {
		reasons[group.Reason] = group.IDs
	}
	if !slices.Equal(reasons[model.SetAsideAnswered], []string{answered.ID}) ||
		!slices.Equal(reasons[model.SetAsideExempt], []string{actor.ID}) ||
		!slices.Equal(reasons[model.SetAsideRetired], []string{old.ID}) {
		t.Errorf("set aside = %v", reasons)
	}
}
