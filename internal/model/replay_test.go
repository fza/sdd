package model_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestReplayText_DropsTheSentencesThatNameAReference(t *testing.T) {
	const (
		earlier = "20260101-100000-d-tac-aaa"
		other   = "20260101-100001-s-tac-bbb"
	)
	e := farEntry("20260102-100000-d-tac-ccc", []string{"cli/ux"}, earlier)
	e.Closes = []string{other}
	e.Content = "The draft keeps this. It builds on 20260101-100000-d-tac-aaa as planned. Also this stays.\nIt closes s-tac-bbb. A last line stays."
	graph := model.NewGraph([]*model.Entry{farEntry(earlier, nil), farEntry(other, nil), e})
	text := model.ReplayText(graph, e)
	for _, gone := range []string{"d-tac-aaa", "s-tac-bbb", "builds on", "closes"} {
		if strings.Contains(text, gone) {
			t.Fatalf("replayed text %q should hold no sentence that names a reference", text)
		}
	}
	for _, kept := range []string{"The draft keeps this.", "Also this stays.", "A last line stays.", "topics: cli/ux"} {
		if !strings.Contains(text, kept) {
			t.Fatalf("replayed text %q should keep every other sentence and the topics", text)
		}
	}
}

func TestReplayNeededAndPopulation_ReadOnlyEarlierEntries(t *testing.T) {
	earlier := farEntry("20260101-100000-d-tac-aaa", nil)
	actor := farEntry("20260101-100001-s-tac-act", nil)
	actor.Kind = model.KindActor
	shipped := farEntry("20260101-100002-s-tac-emb", nil)
	shipped.Embedded = true
	later := farEntry("20260103-100000-d-tac-lat", nil)
	e := farEntry("20260102-100000-d-tac-sub", nil, earlier.ID, actor.ID, shipped.ID, later.ID, "20260101-000000-d-tac-gon")
	graph := model.NewGraph([]*model.Entry{earlier, actor, shipped, later, e})
	if needed := model.ReplayNeeded(graph, e); !slices.Equal(needed, []string{earlier.ID}) {
		t.Fatalf("needed %v: only a held, earlier, judged entry is needed", needed)
	}
	if population := entryIDs(model.ReplayPopulation(graph, e)); !slices.Equal(population, []string{earlier.ID}) {
		t.Fatalf("population %v: a replay reads only judged entries with earlier IDs", population)
	}
}

func TestPickReplays_SpreadsOverTopicAreasAndMonths(t *testing.T) {
	base := farEntry("20250101-000000-d-tac-bas", nil)
	entries := []*model.Entry{base}
	add := func(month, area string, count, refs int) {
		for i := range count {
			var refIDs []string
			for range refs {
				refIDs = append(refIDs, base.ID)
			}
			entries = append(entries, farEntry(fmt.Sprintf("2026%s01-1000%02d-d-tac-%s%d", month, i, area[:2], i), []string{area + "/x"}, refIDs...))
		}
	}
	add("01", "cli", 10, 5)
	add("01", "llm", 1, 1)
	add("02", "cli", 1, 1)
	add("01", "web", 1, 0)
	graph := model.NewGraph(entries)
	picked := model.PickReplays(graph, entries, nil, 3)
	if len(picked) != 3 {
		t.Fatalf("picked %d: three entries should be picked", len(picked))
	}
	areas := map[string]bool{}
	for _, e := range picked {
		areas[e.ID[:6]+e.Topics[0].Components[0]] = true
		if strings.Contains(e.ID, "-we") {
			t.Fatal("an entry without a needed entry has nothing to replay")
		}
	}
	if len(areas) != 3 {
		t.Fatalf("picked %v: each topic area and month should give one before any gives two", entryIDs(picked))
	}
	done := map[string]bool{}
	for _, e := range picked {
		done[e.ID] = true
	}
	for _, e := range model.PickReplays(graph, entries, done, 20) {
		if done[e.ID] {
			t.Fatalf("%s was replayed already", e.ID)
		}
	}
}

func TestReplayLabels_ReferencesArePositiveTheRestWeakNegatives(t *testing.T) {
	checks := []model.ReplayCheck{
		{Subject: "s1", Judge: "j", Revision: "r", Candidates: []model.ReplayCandidate{{ID: "a", Score: 200, Needed: true}, {ID: "b", Score: 10}}},
		{Subject: "s2", Judge: "j", Revision: "old", Candidates: []model.ReplayCandidate{{ID: "c", Needed: true}}},
		{Subject: "s3", Judge: "other", Revision: "r", Candidates: []model.ReplayCandidate{{ID: "d", Needed: true}}},
	}
	labels := model.ReplayLabels(checks, "j", "r")
	if len(labels) != 2 {
		t.Fatalf("labels %+v: only the judge's checks at its revision count", labels)
	}
	if labels[0].Kind != model.LabelPositive || labels[0].Weight != model.WeightReferenced || labels[0].Source != model.SourceCheck || labels[0].Score != 200 {
		t.Fatalf("label %+v: a needed candidate should be a full positive of a replayed check", labels[0])
	}
	if labels[1].Kind != model.LabelNegative || labels[1].Weight != model.ReplayNegativeWeight || labels[1].Weight >= model.WeightReferenced {
		t.Fatalf("label %+v: an unreferenced candidate should be a negative of low weight", labels[1])
	}
}
