package model_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestDraftMoved_WhenFewerThanThreeQuartersOfTheNearestStay(t *testing.T) {
	var judged []string
	for i := range 20 {
		judged = append(judged, fmt.Sprintf("e%02d", i))
	}
	mark := &model.DraftMark{Nearest: judged}
	for _, tc := range []struct {
		name  string
		stay  int
		moved bool
	}{
		{"all stay", 20, false},
		{"fifteen stay", 15, false},
		{"fourteen stay", 14, true},
		{"half stay", 10, true},
		{"nine stay", 9, true},
		{"none stay", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := slices.Clone(judged[:tc.stay])
			for i := range 20 - tc.stay {
				now = append(now, fmt.Sprintf("n%02d", i))
			}
			if got := model.DraftMoved(mark, now); got != tc.moved {
				t.Errorf("moved %v with %d of 20 nearest staying", got, tc.stay)
			}
		})
	}
	if model.DraftMoved(nil, nil) || model.DraftMoved(&model.DraftMark{}, []string{"x"}) {
		t.Errorf("without a list of nearest entries the draft should never move")
	}
}

// keptJudgementsGraph has a low-judged entry per rule, a bearing one and one
// no rule reaches.
func keptJudgementsGraph() (*model.Graph, []*model.Entry) {
	entries := []*model.Entry{
		farEntry("top", nil),
		farEntry("topic", []string{"cli/ux"}),
		farEntry("hop", nil, "ref"),
		farEntry("ref", nil),
		farEntry("bearing", []string{"cli/ux"}),
		farEntry("quiet", nil),
	}
	for i := range 25 {
		entries = append(entries, farEntry(fmt.Sprintf("o%02d", i), nil))
	}
	return model.NewGraph(entries), entries
}

func TestSecondLook_NamesTheLowJudgementsEachRuleReaches(t *testing.T) {
	graph, live := keptJudgementsGraph()
	kept := map[string]uint8{"top": 10, "topic": 10, "hop": 10, "bearing": 250, "quiet": 10, "o24": 10}
	order := []string{"bearing", "top"}
	for i := range 25 {
		order = append(order, fmt.Sprintf("o%02d", i))
	}
	base := model.SecondLook{Graph: graph, Live: live, Kept: kept, Order: order, Previous: &model.DraftMark{}}

	t.Run("best of the merged order", func(t *testing.T) {
		if due := base.Due(); !slices.Equal(due, []string{"top"}) {
			t.Fatalf("due %v: only a low judgement among the first 20 should be judged again", due)
		}
	})
	t.Run("a gained topic", func(t *testing.T) {
		look := base
		look.Current = model.DraftMark{Topics: []string{"cli/ux"}}
		if due := look.Due(); !slices.Equal(due, []string{"top", "topic"}) {
			t.Fatalf("due %v: a low judgement under a gained topic should be judged again, a bearing one never", due)
		}
		look.Previous = &model.DraftMark{Topics: []string{"cli/ux"}}
		if due := look.Due(); slices.Contains(due, "topic") {
			t.Fatalf("due %v: a topic the draft held before gains nothing", due)
		}
	})
	t.Run("a gained reference", func(t *testing.T) {
		look := base
		look.Current = model.DraftMark{References: []string{"ref"}}
		if due := look.Due(); !slices.Equal(due, []string{"hop", "top"}) {
			t.Fatalf("due %v: a low judgement one reference step from a gained reference should be judged again", due)
		}
	})
	t.Run("the draft moved", func(t *testing.T) {
		look := base
		look.Moved = true
		if due := look.Due(); !slices.Equal(due, []string{"hop", "o24", "quiet", "top", "topic"}) {
			t.Fatalf("due %v: every low judgement and no bearing one should be judged again", due)
		}
	})
	t.Run("thresholds decide what bears", func(t *testing.T) {
		look := base
		look.Kept = map[string]uint8{"top": 200}
		look.Thresholds = model.RelevanceThresholds{Valid: true, Relevant: 0.9}
		if due := look.Due(); !slices.Equal(due, []string{"top"}) {
			t.Fatalf("due %v: a score below the relevant threshold is a low judgement", due)
		}
		look.Thresholds = model.RelevanceThresholds{}
		if due := look.Due(); len(due) != 0 {
			t.Fatalf("due %v: without thresholds a score at or above %.1f bears", due, model.FallbackMarkScore)
		}
	})
}

// The draft as judged is the first check's draft until a check marks a
// moved one; the latest check's draft is the one the next compares with.
func TestReading_KeepsTheDraftAsJudged(t *testing.T) {
	var reading model.Reading
	first := &model.DraftMark{Topics: []string{"a"}, Nearest: []string{"x"}}
	reading.Fold(model.ReadPayload{Draft: first})
	second := &model.DraftMark{Topics: []string{"b"}, Nearest: []string{"y"}}
	reading.Fold(model.ReadPayload{Draft: second})
	if reading.JudgedDraft() != first || reading.LastDraft() != second {
		t.Fatalf("the first draft should stay the draft as judged and the second be the last")
	}
	moved := &model.DraftMark{Nearest: []string{"z"}, Judged: true}
	reading.Fold(model.ReadPayload{Draft: moved})
	if reading.JudgedDraft() != moved {
		t.Fatalf("a moved draft should become the draft as judged")
	}
}
