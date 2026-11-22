package model_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func heldLines(ids ...string) []model.Line {
	var lines []model.Line
	for _, id := range ids {
		lines = append(lines, model.Line{Entry: &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindGap}})
	}
	return lines
}

// A late score places a held line as a graph check places it: likely
// bearing, set aside or still held; a line already scored keeps its place.
func TestScoreHeld_PlacesHeldLinesByLateScores(t *testing.T) {
	held := heldLines("high", "low", "mid", "none")
	scored := 0.3
	held = append(held, model.Line{Entry: &model.Entry{ID: "kept", Kind: model.KindGap}, Score: &scored})
	scores := map[string]uint8{"high": 250, "low": 10, "mid": 150, "kept": 255}

	page := model.ScoreHeld(held, scores, model.RelevanceThresholds{Valid: true, SetAside: 0.2, Relevant: 0.9}, 0)
	if !slices.Equal(lineIDs(page.Bearing), []string{"high"}) || !page.Bearing[0].Marked {
		t.Fatalf("bearing %v: a late score at or above the relevant threshold should mark its line", lineIDs(page.Bearing))
	}
	if !slices.Equal(page.SetAside, []string{"low"}) {
		t.Fatalf("set aside %v: a late score below the set-aside threshold should set its line aside", page.SetAside)
	}
	if !slices.Equal(lineIDs(page.Held), []string{"mid", "none", "kept"}) || page.Held[0].Score == nil || *page.Held[2].Score != scored {
		t.Fatalf("held %v: the rest should stay held in order, a scored line with its first score", lineIDs(page.Held))
	}
}

// Without valid thresholds a late score of 0.5 or more marks its line only
// while the check marks fewer than five.
func TestScoreHeld_MarksWithoutThresholdsUpToFive(t *testing.T) {
	held := heldLines("a", "b", "c")
	scores := map[string]uint8{"a": 200, "b": 250, "c": 100}
	page := model.ScoreHeld(held, scores, model.RelevanceThresholds{}, 4)
	if !slices.Equal(lineIDs(page.Bearing), []string{"b"}) {
		t.Fatalf("bearing %v: one mark is left, for the best late score", lineIDs(page.Bearing))
	}
	if !slices.Equal(lineIDs(page.Held), []string{"a", "c"}) || page.Held[0].Marked {
		t.Fatalf("held %v: a line past the five marks should stay held unmarked", lineIDs(page.Held))
	}
	if len(page.SetAside) != 0 {
		t.Fatalf("set aside %v: without thresholds nothing is set aside", page.SetAside)
	}
}
