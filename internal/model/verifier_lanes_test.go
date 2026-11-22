package model_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func placesOf(slots []model.VerifierSlot) map[string]model.VerifierSlot {
	places := map[string]model.VerifierSlot{}
	for _, slot := range slots {
		places[slot.ID] = slot
	}
	return places
}

func slotIDs(slots []model.VerifierSlot) []string {
	ids := make([]string, len(slots))
	for i, slot := range slots {
		ids[i] = slot.ID
	}
	return ids
}

// The declared targets, the drafter's full reads and every listed entry
// closing or superseding a target, or closed or superseded by one, are
// served in full however many there are; a verdict's entries come as
// briefs; an entry one ref away from a target is listed by title and first
// summary sentence whatever its rank.
func TestPlaceVerifierReading_ServesThePrioritySetsInFull(t *testing.T) {
	const (
		target     = "20260101-000000-d-tac-tgt"
		outbound   = "20260101-000001-s-tac-out"
		inbound    = "20260101-000002-s-tac-inb"
		superseded = "20260101-000004-d-tac-old"
		full       = "20260101-000007-s-tac-ful"
		verdict    = "20260101-000008-s-tac-ver"
		plain      = "20260101-000009-s-tac-pln"
	)
	entries := []*model.Entry{
		{ID: target, Type: model.TypeDecision, Kind: model.KindDirective, Refs: []model.Ref{{ID: outbound}}, Supersedes: []string{superseded}},
		{ID: outbound, Type: model.TypeSignal, Kind: model.KindGap},
		{ID: inbound, Type: model.TypeSignal, Kind: model.KindGap, Refs: []model.Ref{{ID: target}}},
		{ID: superseded, Type: model.TypeDecision, Kind: model.KindDirective},
		{ID: full, Type: model.TypeSignal, Kind: model.KindGap},
		{ID: verdict, Type: model.TypeSignal, Kind: model.KindGap},
		{ID: plain, Type: model.TypeSignal, Kind: model.KindGap},
	}
	listed := []string{plain, outbound, inbound, superseded, full, verdict}
	var closers []string
	for i := range model.DeepTopRanked + 5 {
		id := fmt.Sprintf("20260102-%06d-s-tac-cls", i)
		entries = append(entries, &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindDone, Closes: []string{target}})
		closers = append(closers, id)
		listed = append(listed, id)
	}
	g := model.NewGraph(entries)
	draft := model.VerifierDraft{Targets: []string{target}}

	slots := model.PlaceVerifierReading(g, draft, model.VerifierReading{Listed: listed, Full: []string{full}, Verdicts: []string{verdict}})
	places := placesOf(slots)
	want := map[string]model.VerifierRole{target: model.VerifierRoleTarget, full: model.VerifierRoleRead, superseded: model.VerifierRoleLifecycle}
	for _, id := range closers {
		want[id] = model.VerifierRoleLifecycle
	}
	for id, role := range want {
		if slot, ok := places[id]; !ok || slot.Place != model.VerifierPlaceFull || slot.Role != role {
			t.Errorf("%s placed %+v, want served in full as %s", id, slot, role)
		}
	}
	for _, id := range []string{outbound, inbound} {
		if slot := places[id]; slot.Place != model.VerifierPlaceSummary || slot.Role != model.VerifierRoleHop {
			t.Errorf("%s placed %+v, want listed with its first summary sentence as one ref from a target", id, slot)
		}
	}
	if slot := places[verdict]; slot.Place != model.VerifierPlaceBrief || slot.Role != model.VerifierRoleVerdict {
		t.Errorf("verdict entry placed %+v, want a brief given a verdict", slot)
	}
	if slot := places[plain]; slot.Role != model.VerifierRoleRanked || slot.Place != model.VerifierPlaceFull {
		t.Errorf("the one ranked entry placed %+v, want top-ranked in full", slot)
	}
	if len(slots) != len(listed)+1 {
		t.Fatalf("%d slots for %d listed entries and one target: each entry once", len(slots), len(listed))
	}
	if got := slotIDs(slots)[:2]; !slices.Equal(got, []string{target, full}) {
		t.Fatalf("reading opens with %v, want the target, then the full read", got)
	}
}

// The ranked entries go by falling score: the top ones served in full, the
// next listed with their first summary sentence, the rest by title.
func TestPlaceVerifierReading_CutsTheRankedEntriesByScore(t *testing.T) {
	count := model.DeepTopRanked + model.DeepSummaryRanked + 5
	var entries []*model.Entry
	var listed []string
	scores := map[string]uint8{}
	for i := range count {
		id := fmt.Sprintf("20260101-%06d-s-tac-r%02d", i, i)
		entries = append(entries, &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindGap})
		listed = append(listed, id)
		scores[id] = uint8(i)
	}
	slots := model.PlaceVerifierReading(model.NewGraph(entries), model.VerifierDraft{}, model.VerifierReading{Listed: listed, Scores: scores})
	if len(slots) != count {
		t.Fatalf("%d slots for %d ranked entries", len(slots), count)
	}
	for i, slot := range slots {
		want := model.VerifierPlaceTitle
		switch {
		case i < model.DeepTopRanked:
			want = model.VerifierPlaceFull
		case i < model.DeepTopRanked+model.DeepSummaryRanked:
			want = model.VerifierPlaceSummary
		}
		if slot.ID != listed[count-1-i] || slot.Place != want || slot.Hidden {
			t.Fatalf("slot %d = %+v, want %s placed %d: falling score, cut at %d and %d", i, slot, listed[count-1-i], want, model.DeepTopRanked, model.DeepTopRanked+model.DeepSummaryRanked)
		}
	}
}

// An unscored entry is served in full only while every ranked entry fits the
// top: past it, only a score ranks an entry among the top.
func TestPlaceVerifierReading_ServesUnscoredEntriesInFullOnlyWhenAllFit(t *testing.T) {
	var entries []*model.Entry
	var listed []string
	for i := range model.DeepTopRanked + 1 {
		id := fmt.Sprintf("20260101-%06d-s-tac-u%02d", i, i)
		entries = append(entries, &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindGap})
		listed = append(listed, id)
	}
	g := model.NewGraph(entries)
	for _, slot := range model.PlaceVerifierReading(g, model.VerifierDraft{}, model.VerifierReading{Listed: listed[:model.DeepTopRanked]}) {
		if slot.Place != model.VerifierPlaceFull {
			t.Fatalf("slot %+v: ranked entries that all fit the top are served in full unscored", slot)
		}
	}
	slots := model.PlaceVerifierReading(g, model.VerifierDraft{}, model.VerifierReading{Listed: listed, Scores: map[string]uint8{listed[3]: 1}})
	for _, slot := range slots {
		if want := slot.ID == listed[3]; (slot.Place == model.VerifierPlaceFull) != want {
			t.Fatalf("slot %+v: past the top only a scored entry is served in full", slot)
		}
	}
}

// A done entry with a score ranks by it, last only among equal scores and
// among the unscored, and is never served in full.
func TestPlaceVerifierReading_RanksDoneEntriesByScoreAndListsThem(t *testing.T) {
	const (
		done     = "20260101-000000-s-tac-don"
		gap      = "20260101-000001-s-tac-gap"
		tiedDone = "20260101-000002-s-tac-tdn"
		tiedGap  = "20260101-000003-s-tac-tgp"
		openDone = "20260101-000004-s-tac-odn"
		unscored = "20260101-000005-s-tac-uns"
	)
	g := model.NewGraph([]*model.Entry{
		{ID: done, Type: model.TypeSignal, Kind: model.KindDone},
		{ID: gap, Type: model.TypeSignal, Kind: model.KindGap},
		{ID: tiedDone, Type: model.TypeSignal, Kind: model.KindDone},
		{ID: tiedGap, Type: model.TypeSignal, Kind: model.KindGap},
		{ID: openDone, Type: model.TypeSignal, Kind: model.KindDone},
		{ID: unscored, Type: model.TypeSignal, Kind: model.KindGap},
	})
	reading := model.VerifierReading{
		Listed: []string{openDone, unscored, tiedDone, tiedGap, gap, done},
		Scores: map[string]uint8{done: 250, gap: 1, tiedDone: 100, tiedGap: 100},
	}
	slots := model.PlaceVerifierReading(g, model.VerifierDraft{}, reading)
	if want := []string{done, tiedGap, tiedDone, gap, unscored, openDone}; !slices.Equal(slotIDs(slots), want) {
		t.Fatalf("order %v, want %v", slotIDs(slots), want)
	}
	if slots[0].Place == model.VerifierPlaceFull {
		t.Fatalf("done entry placed %+v, want listed, never served in full", slots[0])
	}
}

// Once calibrated, a ranked entry scoring below the set-aside threshold is
// listed by title as hidden, last; the audit share of them returns blind
// among the ranked entries.
func TestPlaceVerifierReading_HidesWhatScoresBelowTheThreshold(t *testing.T) {
	var entries []*model.Entry
	var listed []string
	scores := map[string]uint8{}
	for i := range 100 {
		id := fmt.Sprintf("20260101-%06d-s-tac-low", i)
		entries = append(entries, &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindGap})
		listed = append(listed, id)
		scores[id] = 1
	}
	const high = "20260102-000000-s-tac-hig"
	entries = append(entries, &model.Entry{ID: high, Type: model.TypeSignal, Kind: model.KindGap})
	listed = append(listed, high)
	scores[high] = 250
	reading := model.VerifierReading{Listed: listed, Scores: scores, Thresholds: model.RelevanceThresholds{Valid: true, SetAside: 0.5, Relevant: 0.9}, Seed: "seed"}
	g := model.NewGraph(entries)

	slots := model.PlaceVerifierReading(g, model.VerifierDraft{}, reading)
	hidden := 0
	for i, slot := range slots {
		if slot.Hidden {
			hidden++
			if slot.Place != model.VerifierPlaceTitle || i < len(slots)-97 {
				t.Fatalf("hidden slot %d = %+v: hidden entries go last, by title", i, slot)
			}
		}
	}
	if hidden != 97 || len(slots) != 101 {
		t.Fatalf("%d hidden of %d: ceil(3%%) of the 100 below the threshold should return blind", hidden, len(slots))
	}
	again := model.PlaceVerifierReading(g, model.VerifierDraft{}, reading)
	if !slices.Equal(slotIDs(again), slotIDs(slots)) {
		t.Fatal("one seed should place the same samples at the same positions")
	}
	uncalibrated := reading
	uncalibrated.Thresholds = model.RelevanceThresholds{}
	for _, slot := range model.PlaceVerifierReading(g, model.VerifierDraft{}, uncalibrated) {
		if slot.Hidden {
			t.Fatalf("%s hidden without a calibration", slot.ID)
		}
	}
}

// Once calibrated, the threshold replaces the summary count: every ranked
// entry past the top that scores at or above it is listed with its first
// summary sentence, however many there are.
func TestPlaceVerifierReading_ListsEverythingAboveTheThresholdWithItsSummary(t *testing.T) {
	count := model.DeepTopRanked + model.DeepSummaryRanked + 5
	var entries []*model.Entry
	var listed []string
	scores := map[string]uint8{}
	for i := range count {
		id := fmt.Sprintf("20260101-%06d-s-tac-a%03d", i, i)
		entries = append(entries, &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindGap})
		listed = append(listed, id)
		scores[id] = 200
	}
	reading := model.VerifierReading{Listed: listed, Scores: scores, Thresholds: model.RelevanceThresholds{Valid: true, SetAside: 0.5, Relevant: 0.9}, Seed: "seed"}
	slots := model.PlaceVerifierReading(model.NewGraph(entries), model.VerifierDraft{}, reading)
	for i, slot := range slots {
		want := model.VerifierPlaceSummary
		if i < model.DeepTopRanked {
			want = model.VerifierPlaceFull
		}
		if slot.Place != want || slot.Hidden {
			t.Fatalf("slot %d = %+v, want placed %d: above the threshold nothing is listed by title alone", i, slot, want)
		}
	}
}

func TestVerifierUnscored_AsksForScoresOnlyWhereTheRankDecides(t *testing.T) {
	var slots []model.VerifierSlot
	for i := range model.DeepTopRanked {
		slots = append(slots, model.VerifierSlot{ID: fmt.Sprintf("r%d", i), Role: model.VerifierRoleRanked})
	}
	slots = append(slots, model.VerifierSlot{ID: "target", Role: model.VerifierRoleTarget})
	if got := model.VerifierUnscored(slots, model.VerifierReading{}); len(got) != 0 {
		t.Fatalf("unscored %v: ranked entries that all fit the top need no score", got)
	}
	if got := model.VerifierUnscored(slots, model.VerifierReading{Scores: map[string]uint8{"r0": 1}, Thresholds: model.RelevanceThresholds{Valid: true}}); len(got) != model.DeepTopRanked-1 || slices.Contains(got, "r0") || slices.Contains(got, "target") {
		t.Fatalf("unscored %v: a calibration hides by score, so every unscored ranked entry needs one", got)
	}
	slots = append(slots, model.VerifierSlot{ID: "extra", Role: model.VerifierRoleRanked})
	if got := model.VerifierUnscored(slots, model.VerifierReading{}); len(got) != model.DeepTopRanked+1 {
		t.Fatalf("unscored %v: past the top cut the rank decides, so every ranked entry needs a score", got)
	}
	var hops []model.VerifierSlot
	for i := range model.DeepHopListed + 1 {
		hops = append(hops, model.VerifierSlot{ID: fmt.Sprintf("h%d", i), Role: model.VerifierRoleHop})
	}
	if got := model.VerifierUnscored(hops[:model.DeepHopListed], model.VerifierReading{Thresholds: model.RelevanceThresholds{Valid: true}}); len(got) != 0 {
		t.Fatalf("unscored %v: ref neighbours that all fit their ceiling need no score", got)
	}
	if got := model.VerifierUnscored(hops, model.VerifierReading{}); len(got) != model.DeepHopListed+1 {
		t.Fatalf("unscored %v: past their ceiling the rank decides which ref neighbours are listed", got)
	}
}

// Past their ceilings, the ref neighbours and the entries listed by title
// are counted, not listed, the lowest-ranked first.
func TestPlaceVerifierReading_CountsWhatPassesTheCeilings(t *testing.T) {
	const target = "20260101-000000-d-tac-tgt"
	entries := []*model.Entry{{ID: target, Type: model.TypeDecision, Kind: model.KindDirective}}
	var listed, hops []string
	scores := map[string]uint8{}
	for i := range model.DeepHopListed + 3 {
		id := fmt.Sprintf("20260102-%06d-s-tac-hop", i)
		entries = append(entries, &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindGap, Refs: []model.Ref{{ID: target}}})
		listed, hops = append(listed, id), append(hops, id)
		scores[id] = uint8(i)
	}
	rankedCount := model.DeepTopRanked + model.DeepSummaryRanked + model.DeepTitleListed + 4
	for i := range rankedCount {
		id := fmt.Sprintf("20260103-%06d-s-tac-rnk", i)
		entries = append(entries, &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindGap})
		listed = append(listed, id)
		scores[id] = uint8(i % 256)
	}
	slots := model.PlaceVerifierReading(model.NewGraph(entries), model.VerifierDraft{Targets: []string{target}}, model.VerifierReading{Listed: listed, Scores: scores})
	counted := map[model.VerifierRole]int{}
	for _, slot := range slots {
		if slot.Place == model.VerifierPlaceCounted {
			counted[slot.Role]++
		}
	}
	if counted[model.VerifierRoleHop] != 3 || counted[model.VerifierRoleRanked] != 4 {
		t.Fatalf("counted %v: 3 ref neighbours past %d and 4 titles past %d", counted, model.DeepHopListed, model.DeepTitleListed)
	}
	places := placesOf(slots)
	for i := range 3 {
		if places[hops[i]].Place != model.VerifierPlaceCounted {
			t.Fatalf("ref neighbour %s placed %+v: the lowest-scored past the ceiling should be counted", hops[i], places[hops[i]])
		}
	}
}

// Past the judge, unscored entries rank by their similarity to the draft,
// the most similar first, then the rest in listed order; similarity alone
// serves none in full.
func TestPlaceVerifierReading_RanksUnscoredEntriesBySimilarity(t *testing.T) {
	var entries []*model.Entry
	var listed []string
	for i := range model.DeepTopRanked + 3 {
		id := fmt.Sprintf("20260101-%06d-s-tac-s%02d", i, i)
		entries = append(entries, &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindGap})
		listed = append(listed, id)
	}
	scored, near, far := listed[4], listed[6], listed[1]
	reading := model.VerifierReading{
		Listed:     listed,
		Scores:     map[string]uint8{scored: 10},
		Similarity: map[string]float64{near: 0.9, far: 0.2},
	}
	slots := model.PlaceVerifierReading(model.NewGraph(entries), model.VerifierDraft{}, reading)
	want := []string{scored, near, far, listed[0], listed[2], listed[3], listed[5], listed[7]}
	if !slices.Equal(slotIDs(slots), want) {
		t.Fatalf("order %v, want %v: the scored first, then by falling similarity, then in listed order", slotIDs(slots), want)
	}
	for _, slot := range slots[1:] {
		if slot.Place == model.VerifierPlaceFull {
			t.Fatalf("slot %+v: similarity alone should serve no entry in full", slot)
		}
	}
}

// A widened reading doubles the top served in full and the summary lines.
func TestPlaceVerifierReading_WidenedDoublesTheTiers(t *testing.T) {
	count := 2*(model.DeepTopRanked+model.DeepSummaryRanked) + 5
	var entries []*model.Entry
	var listed []string
	scores := map[string]uint8{}
	for i := range count {
		id := fmt.Sprintf("20260101-%06d-s-tac-w%03d", i, i)
		entries = append(entries, &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindGap})
		listed = append(listed, id)
		scores[id] = uint8(255 - i%200)
	}
	slots := model.PlaceVerifierReading(model.NewGraph(entries), model.VerifierDraft{}, model.VerifierReading{Listed: listed, Scores: scores, Widened: true})
	places := map[model.VerifierPlace]int{}
	for _, slot := range slots {
		places[slot.Place]++
	}
	if places[model.VerifierPlaceFull] != 2*model.DeepTopRanked || places[model.VerifierPlaceSummary] != 2*model.DeepSummaryRanked {
		t.Fatalf("places %v: a widened reading serves %d in full and lists %d with their summary", places, 2*model.DeepTopRanked, 2*model.DeepSummaryRanked)
	}
	var top []model.VerifierSlot
	for i := range 2 * model.DeepTopRanked {
		top = append(top, model.VerifierSlot{ID: fmt.Sprintf("r%d", i), Role: model.VerifierRoleRanked})
	}
	if got := model.VerifierUnscored(top, model.VerifierReading{Widened: true}); len(got) != 0 {
		t.Fatalf("unscored %v: ranked entries that all fit the widened top need no score", got)
	}
}

// The ranking sources count the entries the rank places by what ranked
// them: the judge's score, similarity to the draft, or listed order.
func TestVerifierRankingSources_CountsWhatRankedEachEntry(t *testing.T) {
	slots := []model.VerifierSlot{
		{ID: "target", Role: model.VerifierRoleTarget},
		{ID: "a", Role: model.VerifierRoleRanked},
		{ID: "b", Role: model.VerifierRoleHop},
		{ID: "c", Role: model.VerifierRoleRanked},
		{ID: "d", Role: model.VerifierRoleRanked},
	}
	reading := model.VerifierReading{Scores: map[string]uint8{"a": 1, "target": 2}, Similarity: map[string]float64{"b": 0.5}}
	got := model.VerifierRankingSources(slots, reading)
	if got != (model.RankingSources{Judged: 1, Similar: 1, Listed: 2}) {
		t.Fatalf("sources %+v: one judged, one by similarity, two in listed order; the target is not ranked", got)
	}
	for name, tc := range map[string]struct {
		sources model.RankingSources
		want    string
	}{
		"judge":      {model.RankingSources{Judged: 3}, "judge"},
		"similarity": {model.RankingSources{Similar: 3}, "similarity"},
		"mixed":      {model.RankingSources{Judged: 1, Listed: 1}, "mixed"},
		"listed":     {model.RankingSources{Listed: 2}, "listed order"},
		"none":       {model.RankingSources{}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.sources.Kind(); got != tc.want {
				t.Fatalf("Kind = %q, want %q", got, tc.want)
			}
		})
	}
}
