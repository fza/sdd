package model_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func farEntry(id string, topics []string, refs ...string) *model.Entry {
	e := &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindGap}
	for _, label := range topics {
		path, err := model.ParseTopicPath(label)
		if err != nil {
			panic(err)
		}
		e.Topics = append(e.Topics, path)
	}
	for _, ref := range refs {
		e.Refs = append(e.Refs, model.Ref{ID: ref})
	}
	return e
}

func entryIDs(entries []*model.Entry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	slices.Sort(ids)
	return ids
}

// Two entries are far in every case, of another topic and of none, one short
// of a floor. Each case adds entries: one more far entry gives the floor,
// an entry a rule keeps out gives none.
func TestNewFarFloor_PicksOnlyEntriesFarFromTheDraft(t *testing.T) {
	const reference = "ref"
	draft := model.FarDraft{Subject: "subject", Topics: []string{"cli/ux"}, References: []string{reference}}
	base := func() []*model.Entry {
		return []*model.Entry{
			farEntry(reference, nil),
			farEntry("hop1", nil, reference),
			farEntry("hop2", nil, "hop1"),
			farEntry("far-topic", []string{"docs/site"}),
			farEntry("far-plain", nil),
		}
	}
	annotation := &model.Entry{ID: "note", Type: model.TypeSignal, Kind: model.KindAnnotation, Refs: []model.Ref{{ID: "annotated"}}, AnnotationTopics: []model.AnnotationTopic{{Label: "cli/notes"}}}
	closing := farEntry("closing", nil)
	closing.Closes = []string{reference}
	superseded := farEntry("superseding", nil)
	superseded.Supersedes = []string{"hop1"}
	shipped := farEntry("shipped", nil)
	shipped.Embedded = true
	for _, tc := range []struct {
		name   string
		extra  []*model.Entry
		listed []string
		far    []string
	}{
		{name: "an entry three hops from the reference", extra: []*model.Entry{farEntry("far-hop3", nil, "hop2")}, far: []string{"far-hop3", "far-plain", "far-topic"}},
		{name: "an entry of the draft's topic", extra: []*model.Entry{farEntry("same-topic", []string{"cli/ux"})}},
		{name: "an entry of the draft's topic area", extra: []*model.Entry{farEntry("same-area", []string{"docs", "CLI/output"})}},
		{name: "an entry an annotation puts in the area", extra: []*model.Entry{farEntry("annotated", nil), annotation}},
		{name: "an entry the reference references", extra: []*model.Entry{farEntry("target", nil), farEntry("referencing", nil, "target", reference)}},
		{name: "an entry that closes the reference", extra: []*model.Entry{closing}},
		{name: "an entry two hops away over a supersede", extra: []*model.Entry{superseded}},
		{name: "a candidate of the gather", extra: []*model.Entry{farEntry("listed", nil)}, listed: []string{"listed"}},
		{name: "an entry the binary ships", extra: []*model.Entry{shipped}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := append(base(), tc.extra...)
			floor := model.NewFarFloor(model.NewGraph(entries), entries, draft, append([]string{reference}, tc.listed...))
			var got []string
			if floor != nil {
				got = entryIDs(floor.Far)
			}
			if !slices.Equal(got, tc.far) {
				t.Fatalf("far entries %v: only entries outside the draft's topic areas, its references' neighbourhood and the gather's list qualify", got)
			}
		})
	}
}

func TestNewFarFloor_NeedsThreeFarEntriesAndATopicOrAReference(t *testing.T) {
	entries := []*model.Entry{farEntry("ref", nil), farEntry("a", nil), farEntry("b", nil), farEntry("c", nil), farEntry("near", []string{"cli"})}
	for _, tc := range []struct {
		name    string
		draft   model.FarDraft
		entries []*model.Entry
		floor   bool
	}{
		{name: "a topic and a reference", draft: model.FarDraft{Subject: "s", Topics: []string{"cli"}, References: []string{"ref"}}, entries: entries, floor: true},
		{name: "a topic alone", draft: model.FarDraft{Subject: "s", Topics: []string{"cli"}}, entries: entries, floor: true},
		{name: "a reference alone", draft: model.FarDraft{Subject: "s", References: []string{"ref"}}, entries: entries, floor: true},
		{name: "neither topic nor reference", draft: model.FarDraft{Subject: "s"}, entries: entries},
		{name: "two far entries", draft: model.FarDraft{Subject: "s", Topics: []string{"cli"}}, entries: entries[2:]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			floor := model.NewFarFloor(model.NewGraph(tc.entries), tc.entries, tc.draft, []string{"ref"})
			if (floor != nil) != tc.floor {
				t.Fatalf("floor = %v: a floor needs a draft with a topic or a reference and three far entries", floor != nil)
			}
		})
	}
}

func TestNewFarFloor_PicksAlikeForOneDraft(t *testing.T) {
	var entries []*model.Entry
	for i := range 40 {
		entries = append(entries, farEntry(fmt.Sprintf("e%02d", i), nil))
	}
	graph := model.NewGraph(entries)
	draft := model.FarDraft{Subject: "subject", Topics: []string{"cli"}}
	first := model.NewFarFloor(graph, entries, draft, nil)
	reversed := slices.Clone(entries)
	slices.Reverse(reversed)
	again := model.NewFarFloor(graph, reversed, draft, nil)
	if len(first.Far) != model.FarFloorSize || !slices.Equal(entryIDs(first.Far), entryIDs(again.Far)) {
		t.Fatalf("picks %v and %v: one draft should pick the same three in any population order", entryIDs(first.Far), entryIDs(again.Far))
	}
	draft.Subject = "another subject"
	other := model.NewFarFloor(graph, entries, draft, nil)
	if slices.Equal(entryIDs(first.Far), entryIDs(other.Far)) {
		t.Fatalf("picks %v for two drafts: the draft's subject hash should draw the pick", entryIDs(first.Far))
	}
}

func TestFarFloor_Below(t *testing.T) {
	entries := []*model.Entry{
		farEntry("ref", nil), farEntry("hop1", nil, "ref"), farEntry("hop2", nil, "hop1"), farEntry("hop3", nil, "hop2"),
		farEntry("a", nil), farEntry("b", nil), farEntry("c", nil),
	}
	graph := model.NewGraph(entries)
	floor := model.NewFarFloor(graph, entries, model.FarDraft{Subject: "s", References: []string{"ref"}}, []string{"ref", "hop3"})
	if floor == nil {
		t.Fatal("the fixture should give a floor")
	}
	one, two := []string{"semantic"}, []string{"semantic", "keyword(x)"}
	for _, tc := range []struct {
		name         string
		id           string
		foundBy      []string
		score, floor float64
		below        bool
	}{
		{name: "a score under the floor", id: "hop3", foundBy: one, score: 0.1, floor: 0.2, below: true},
		{name: "a score at the floor", id: "hop3", foundBy: one, score: 0.2, floor: 0.2, below: true},
		{name: "a score over the floor", id: "hop3", foundBy: one, score: 0.21, floor: 0.2},
		{name: "a candidate one hop from a reference", id: "hop1", foundBy: one, score: 0, floor: 0.2},
		{name: "a candidate two hops from a reference", id: "hop2", foundBy: one, score: 0, floor: 0.2},
		{name: "a candidate two reads found", id: "hop3", foundBy: two, score: 0, floor: 0.2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := model.GatherCandidate{Entry: graph.ByID[tc.id], FoundBy: tc.foundBy}
			if got := floor.Below(candidate, tc.score, tc.floor); got != tc.below {
				t.Fatalf("below = %v", got)
			}
		})
	}
}

func TestPlacePrefiltered_SetsAsideBelowTheFarFloor(t *testing.T) {
	candidates := []model.GatherCandidate{{Entry: &model.Entry{ID: "high"}}, {Entry: &model.Entry{ID: "mid"}}, {Entry: &model.Entry{ID: "low"}}, {Entry: &model.Entry{ID: "floored-b"}}, {Entry: &model.Entry{ID: "floored-a"}}}
	scores := map[string]uint8{"high": 250, "mid": 120, "low": 5, "floored-b": 30, "floored-a": 240}
	below := map[string]bool{"floored-a": true, "floored-b": true}
	for _, tc := range []struct {
		name       string
		thresholds model.RelevanceThresholds
		toJudge    []string
		relevant   []string
		setAside   []string
	}{
		{name: "without thresholds", toJudge: []string{"high", "mid", "low"}},
		{name: "with thresholds", thresholds: model.RelevanceThresholds{Valid: true, SetAside: 0.1, Relevant: 0.9}, toJudge: []string{"mid"}, relevant: []string{"high"}, setAside: []string{"low"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			placed := model.PlacePrefiltered(candidates, scores, below, tc.thresholds, false)
			if !slices.Equal(placed.BelowFloor, []string{"floored-a", "floored-b"}) {
				t.Fatalf("below the floor %v: both verdicts should set their candidate aside, sorted", placed.BelowFloor)
			}
			if !slices.Equal(ids(placed.ToJudge), tc.toJudge) || !slices.Equal(ids(placed.Relevant), tc.relevant) || !slices.Equal(placed.SetAside, tc.setAside) {
				t.Fatalf("to judge %v, relevant %v, set aside %v: a candidate below the floor belongs to no other group", ids(placed.ToJudge), ids(placed.Relevant), placed.SetAside)
			}
		})
	}
}

// A floor verdict rides the reading with its score: the first one a
// candidate got stands, whatever a later record says.
func TestReadingBelowFloor_KeepsTheFirstVerdict(t *testing.T) {
	subject, revised := model.RelevanceSubjectHash("draft"), model.RelevanceSubjectHash("revised draft")
	reading := &model.Reading{}
	reading.Fold(model.ReadPayload{Prefilter: &model.PrefilterPayload{Judge: "j", Subject: subject, Scores: map[string]uint8{"below": 10, "above": 20}, Below: []string{"below"}}})
	reading.Fold(model.ReadPayload{Prefilter: &model.PrefilterPayload{Judge: "j", Subject: subject, Scores: map[string]uint8{"below": 90, "above": 5, "late": 3}, Below: []string{"above", "late"}}})
	if got := reading.BelowFloor("j", subject); !slices.Equal(got, []string{"below", "late"}) {
		t.Fatalf("below the floor %v: a scored candidate keeps its first verdict and a new one brings its own", got)
	}
	if scores, _ := reading.Scores("j", subject); scores["below"] != 90 {
		t.Fatalf("scores %v: the score still folds to the highest", scores)
	}
	if got := reading.BelowFloor("other", subject); got != nil {
		t.Fatalf("below the floor %v for a judge that scored nothing", got)
	}
	if got := reading.BelowFloor("j", revised); got != nil {
		t.Fatalf("below the floor %v for a revised draft: its verdicts answer the draft they were given against", got)
	}
	reading.Fold(model.ReadPayload{Prefilter: &model.PrefilterPayload{Judge: "j", Subject: revised, Scores: map[string]uint8{"above": 1}, Below: []string{"above"}}})
	if got := reading.BelowFloor("j", revised); !slices.Equal(got, []string{"above"}) {
		t.Fatalf("below the floor %v: a revised draft starts its verdicts anew", got)
	}
	var none *model.Reading
	if got := none.BelowFloor("j", subject); got != nil {
		t.Fatalf("below the floor %v without a reading", got)
	}
}

// A verdict the reading holds stands while its candidate is no neighbour of
// a reference and one read finds it: a reference the draft adds later, or a
// second read, takes the candidate back above the floor.
func TestStandingBelowFloor(t *testing.T) {
	entries := []*model.Entry{farEntry("ref", nil), farEntry("hop1", nil, "ref"), farEntry("hop2", nil, "hop1"), farEntry("hop3", nil, "hop2"), farEntry("plain", nil)}
	graph := model.NewGraph(entries)
	one, two := []string{"semantic"}, []string{"semantic", "keyword(x)"}
	held := []string{"hop1", "hop2", "hop3", "plain", "gone"}
	for _, tc := range []struct {
		name       string
		references []string
		foundBy    map[string][]string
		standing   []string
	}{
		{name: "no reference", standing: []string{"hop1", "hop2", "hop3", "plain"}},
		{name: "a reference two hops from two of them", references: []string{"ref"}, standing: []string{"hop3", "plain"}},
		{name: "a second read", foundBy: map[string][]string{"plain": two}, standing: []string{"hop1", "hop2", "hop3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var candidates []model.GatherCandidate
			for _, id := range []string{"hop1", "hop2", "hop3", "plain"} {
				foundBy := one
				if tc.foundBy[id] != nil {
					foundBy = tc.foundBy[id]
				}
				candidates = append(candidates, model.GatherCandidate{Entry: graph.ByID[id], FoundBy: foundBy})
			}
			standing := model.StandingBelowFloor(graph, tc.references, candidates, held)
			var got []string
			for id := range standing {
				got = append(got, id)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.standing) {
				t.Fatalf("standing %v: a held verdict stands only for a candidate the floor does not spare", got)
			}
		})
	}
}
