package model_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

func lineCandidates(ids ...string) []model.GatherCandidate {
	var candidates []model.GatherCandidate
	for _, id := range ids {
		candidates = append(candidates, model.GatherCandidate{Entry: &model.Entry{ID: id}})
	}
	return candidates
}

func lineIDs(lines []model.Line) []string {
	var out []string
	for _, line := range lines {
		out = append(out, line.Entry.ID)
	}
	return out
}

func setAsideIDs(groups []model.GatherSetAside, reason string) []string {
	for _, group := range groups {
		if group.Reason == reason {
			return group.IDs
		}
	}
	return nil
}

func TestPlaceLines_SortsCandidatesIntoThreeClasses(t *testing.T) {
	candidates := lineCandidates("unscored", "low", "mid", "high", "floored", "mid2")
	scores := map[string]uint8{"low": 5, "mid": 100, "high": 250, "floored": 240, "mid2": 120}
	below := map[string]bool{"floored": true}
	for _, tc := range []struct {
		name       string
		thresholds model.RelevanceThresholds
		bearing    []string
		doubt      []string
		setAside   []string
	}{
		{
			name:       "valid thresholds",
			thresholds: model.RelevanceThresholds{Valid: true, SetAside: 0.1, Relevant: 0.9},
			bearing:    []string{"high"},
			doubt:      []string{"mid2", "mid", "unscored"},
			setAside:   []string{"low"},
		},
		{
			name:    "no thresholds",
			bearing: []string{"high"},
			doubt:   []string{"mid2", "mid", "low", "unscored"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			placed := model.PlaceLines(model.LineInput{Candidates: candidates, Scores: scores, BelowFloor: below, Thresholds: tc.thresholds})
			if got := lineIDs(placed.Bearing); !slices.Equal(got, tc.bearing) {
				t.Fatalf("bearing %v: the lines likely bearing should hold the high score only", got)
			}
			if got := lineIDs(placed.Doubt); !slices.Equal(got, tc.doubt) {
				t.Fatalf("doubt %v: scored lines should run by falling score, unscored after", got)
			}
			if got := setAsideIDs(placed.SetAside, model.SetAsideFiltered); !slices.Equal(got, tc.setAside) {
				t.Fatalf("set aside %v: only a valid threshold should set a score aside", got)
			}
			if got := setAsideIDs(placed.SetAside, model.SetAsideFloor); !slices.Equal(got, []string{"floored"}) {
				t.Fatalf("below the floor %v: the far floor should set aside on every path", got)
			}
			if placed.Judged != 5 {
				t.Fatalf("judged %d: every scored candidate should count", placed.Judged)
			}
			for _, line := range placed.Bearing {
				if !line.Marked {
					t.Fatalf("%s: a line likely bearing should carry the mark", line.Entry.ID)
				}
			}
			for _, line := range placed.Doubt {
				if line.Marked {
					t.Fatalf("%s: a line in doubt should carry no mark", line.Entry.ID)
				}
			}
		})
	}
}

// Without learned thresholds the best five scores at 0.5 or more are marked;
// fewer reach it, fewer are marked.
func TestPlaceLines_MarksTheBestFiveWithoutThresholds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		scores  []uint8
		bearing int
	}{
		{name: "many high scores", scores: []uint8{250, 240, 230, 220, 210, 200, 190}, bearing: 5},
		{name: "two reach the line", scores: []uint8{250, 128, 127, 50}, bearing: 2},
		{name: "none reach the line", scores: []uint8{100, 50}, bearing: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ids []string
			scores := map[string]uint8{}
			for i, score := range tc.scores {
				id := fmt.Sprintf("c%d", i)
				ids = append(ids, id)
				scores[id] = score
			}
			placed := model.PlaceLines(model.LineInput{Candidates: lineCandidates(ids...), Scores: scores})
			if len(placed.Bearing) != tc.bearing {
				t.Fatalf("%d marked: the mark should go to the best scores at or above the line, at most five", len(placed.Bearing))
			}
			if got := lineIDs(placed.Bearing); !slices.Equal(got, ids[:tc.bearing]) {
				t.Fatalf("marked %v: the mark should go to the highest scores", got)
			}
		})
	}
}

// In a check the agent judges itself the scored entries keep their place,
// the first five unscored carry the mark and the first fifty unscored lines
// their summary sentence.
func TestPlaceLines_MarksTheFirstUnscoredInACheckTheAgentJudges(t *testing.T) {
	ids := []string{"scored-high", "scored-low"}
	for i := range 80 {
		ids = append(ids, fmt.Sprintf("u%02d", i))
	}
	scores := map[string]uint8{"scored-high": 250, "scored-low": 20}
	placed := model.PlaceLines(model.LineInput{Candidates: lineCandidates(ids...), Scores: scores, SelfJudged: true})
	if got := lineIDs(placed.Bearing); !slices.Equal(got, []string{"scored-high", "u00", "u01", "u02", "u03", "u04"}) {
		t.Fatalf("marked %v: the scored mark keeps its place and the first five unscored follow in candidate order", got)
	}
	if placed.Doubt[0].Entry.ID != "scored-low" || placed.Doubt[1].Entry.ID != "u05" {
		t.Fatalf("doubt starts %v: a scored entry keeps its place before the unscored", lineIDs(placed.Doubt[:2]))
	}
	for _, line := range placed.Doubt {
		position := slices.Index(ids, line.Entry.ID) - 2
		if line.Entry.ID == "scored-low" {
			continue
		}
		if want := position < model.UnjudgedSummaryLines; line.Summary != want {
			t.Fatalf("%s summary %v: the first fifty unscored lines should carry their summary sentence", line.Entry.ID, line.Summary)
		}
	}
	judged := model.PlaceLines(model.LineInput{Candidates: lineCandidates(ids...), Scores: scores})
	for _, line := range judged.Bearing {
		if line.Score == nil {
			t.Fatalf("%s: a check the judge holds should mark no unscored line", line.Entry.ID)
		}
	}
}

// On every path the entries in doubt show as at most 100 lines, 200 for a
// weak judge, the first 30 with their summary sentence, then at most 300
// title lines, and the rest are counted.
func TestPlaceLines_LimitsTheEntriesInDoubtOnEveryPath(t *testing.T) {
	var ids []string
	scores := map[string]uint8{}
	for i := range 450 {
		id := fmt.Sprintf("c%04d", i)
		ids = append(ids, id)
		scores[id] = uint8(100 - i/5)
	}
	for _, tc := range []struct {
		name   string
		input  model.LineInput
		lines  int
		titles int
	}{
		{name: "no thresholds", input: model.LineInput{Scores: scores}, lines: 100, titles: 300},
		{name: "valid thresholds", input: model.LineInput{Scores: scores, Thresholds: model.RelevanceThresholds{Valid: true, SetAside: 0, Relevant: 2}}, lines: 100, titles: 300},
		{name: "judge failed", input: model.LineInput{SelfJudged: true}, lines: 100, titles: 300},
		{name: "judge off", input: model.LineInput{}, lines: 100, titles: 300},
		{name: "weak judge", input: model.LineInput{Scores: scores, Widened: true}, lines: 200, titles: 250},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.input.Candidates = lineCandidates(ids...)
			placed := model.PlaceLines(tc.input)
			if len(placed.Doubt) != tc.lines || len(placed.Titles) != tc.titles {
				t.Fatalf("%d lines and %d titles: the limits should hold", len(placed.Doubt), len(placed.Titles))
			}
			if counted := 450 - len(placed.Bearing) - tc.lines - tc.titles; len(placed.Counted) != counted {
				t.Fatalf("%d counted: what passes the title limit should be counted", len(placed.Counted))
			}
			summaries := 0
			for _, line := range placed.Doubt {
				if line.Summary {
					summaries++
				}
			}
			want := model.DoubtSummaryLines
			if tc.input.SelfJudged {
				want = model.UnjudgedSummaryLines - model.FallbackMarks
			}
			if summaries != want {
				t.Fatalf("%d summary lines: the first lines in doubt should carry their summary sentence", summaries)
			}
		})
	}
}

// A caller's mark makes a line likely bearing with its reason, whatever the
// score; a hop keeps its flag on every class.
func TestPlaceLines_KeepsTheCallersMarksAndHops(t *testing.T) {
	placed := model.PlaceLines(model.LineInput{
		Candidates: lineCandidates("closer", "hop", "other"),
		Scores:     map[string]uint8{"closer": 3, "hop": 40},
		Thresholds: model.RelevanceThresholds{Valid: true, SetAside: 0.1, Relevant: 0.9},
		Marked:     map[string]string{"closer": "closes a target"},
		Hops:       map[string]bool{"hop": true},
	})
	if len(placed.Bearing) != 1 || placed.Bearing[0].Reason != "closes a target" {
		t.Fatalf("bearing %v: a marked candidate should be likely bearing with its reason despite a low score", lineIDs(placed.Bearing))
	}
	if !placed.Doubt[0].Hop || placed.Doubt[0].Entry.ID != "hop" {
		t.Fatalf("doubt %v: the hop should stay in doubt with its flag", lineIDs(placed.Doubt))
	}
}

// A seed draws a blind sample of the set-aside entries into the lines in
// doubt, unscored, and out of the set-aside groups.
func TestPlaceLines_ShowsTheAuditSampleBlind(t *testing.T) {
	var ids []string
	scores := map[string]uint8{}
	for i := range 40 {
		id := fmt.Sprintf("low%02d", i)
		ids = append(ids, id)
		scores[id] = 1
	}
	ids = append(ids, "mid")
	scores["mid"] = 120
	placed := model.PlaceLines(model.LineInput{Candidates: lineCandidates(ids...), Scores: scores, Thresholds: model.RelevanceThresholds{Valid: true, SetAside: 0.1, Relevant: 0.9}, Seed: "seed"})
	if len(placed.Audit) == 0 {
		t.Fatal("no audit sample: a seed should draw one from the set-aside entries")
	}
	for _, id := range placed.Audit {
		if slices.Contains(setAsideIDs(placed.SetAside, model.SetAsideFiltered), id) {
			t.Fatalf("%s: an audit sample should leave the set-aside group", id)
		}
		at := slices.Index(lineIDs(placed.Doubt), id)
		if at < 0 || !placed.Doubt[at].Blind || placed.Doubt[at].Score != nil {
			t.Fatalf("%s: an audit sample should show blind and unscored among the lines in doubt", id)
		}
	}
	if len(placed.Audit)+len(setAsideIDs(placed.SetAside, model.SetAsideFiltered)) != 40 {
		t.Fatal("the audit sample and the set-aside group should together hold every low score")
	}
}

func TestNewerRefiner_NamesTheNewestLiveRefiner(t *testing.T) {
	const (
		base    = "20260101-000000-d-tac-bas"
		older   = "20251231-000000-d-tac-old"
		refines = "20260102-000000-d-tac-ref"
		newest  = "20260103-000000-d-tac-nwe"
		retired = "20260104-000000-d-tac-ret"
		closer  = "20260105-000000-s-tac-cls"
		cites   = "20260106-000000-d-tac-cit"
	)
	at := func(id string) model.Entry {
		stamp, err := time.Parse("20060102-150405", id[:15])
		if err != nil {
			t.Fatal(err)
		}
		return model.Entry{ID: id, Type: model.TypeDecision, Kind: model.KindPlan, Time: stamp}
	}
	entry := func(id string, refs ...model.Ref) *model.Entry {
		e := at(id)
		e.Refs = refs
		return &e
	}
	refining := model.Ref{ID: base, Kind: model.RefKindRefines}
	closing := entry(closer)
	closing.Type, closing.Kind, closing.Closes = model.TypeSignal, model.KindDone, []string{retired}
	g := model.NewGraph([]*model.Entry{
		entry(base), entry(older, refining), entry(refines, refining), entry(newest, refining), entry(retired, refining), closing,
		entry(cites, model.Ref{ID: base, Kind: model.RefKindBuildsOn}),
	})
	if got := model.NewerRefiner(g, base); got != newest {
		t.Fatalf("refiner %q: the newest live entry refining the base should be named", got)
	}
	if got := model.NewerRefiner(g, cites); got != "" {
		t.Fatalf("refiner %q: an entry nobody refines should name none", got)
	}
}

func TestRefHops_TakesBothDirections(t *testing.T) {
	g := model.NewGraph([]*model.Entry{
		{ID: "target", Refs: []model.Ref{{ID: "out"}}},
		{ID: "out"},
		{ID: "in", Refs: []model.Ref{{ID: "target"}}},
		{ID: "far", Refs: []model.Ref{{ID: "in"}}},
	})
	hops := model.RefHops(g, []string{"target"})
	if !hops["out"] || !hops["in"] || hops["far"] {
		t.Fatalf("hops %v: one reference step in either direction, no further", hops)
	}
}
