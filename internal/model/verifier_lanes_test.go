package model_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func gap(id string) *model.Entry {
	return &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindGap}
}

func bearingReasons(placement model.VerifierPlacement) map[string]string {
	reasons := map[string]string{}
	for _, line := range placement.Lines.Bearing {
		reasons[line.Entry.ID] = line.Reason
	}
	return reasons
}

// Only the declared targets come in full. A lifecycle neighbour of a target
// is a marked line with its reason in both directions, listed or not; the
// drafter's full reads and one-ref neighbours are lines, the latter with the
// hop; an entry given a verdict is a line with its summary sentence.
func TestPlaceVerifierReading_ServesOnlyTheTargetsInFull(t *testing.T) {
	const (
		target     = "20260101-000000-d-tac-tgt"
		outbound   = "20260101-000001-s-tac-out"
		inbound    = "20260101-000002-s-tac-inb"
		superseded = "20260101-000004-d-tac-old"
		full       = "20260101-000007-s-tac-ful"
		verdict    = "20260101-000008-s-tac-ver"
		plain      = "20260101-000009-s-tac-pln"
		closer     = "20260102-000000-s-tac-cls"
	)
	targetEntry := &model.Entry{ID: target, Type: model.TypeDecision, Kind: model.KindDirective, Refs: []model.Ref{{ID: outbound}}, Supersedes: []string{superseded}}
	closing := &model.Entry{ID: closer, Type: model.TypeSignal, Kind: model.KindDone, Closes: []string{target}}
	inboundEntry := gap(inbound)
	inboundEntry.Refs = []model.Ref{{ID: target}}
	g := model.NewGraph([]*model.Entry{targetEntry, gap(outbound), inboundEntry, {ID: superseded, Type: model.TypeDecision, Kind: model.KindDirective}, gap(full), gap(verdict), gap(plain), closing})
	reading := model.VerifierReading{
		Listed: []string{plain, outbound, inbound, verdict}, Full: []string{full}, Verdicts: []string{verdict},
		Scores: map[string]uint8{plain: 10, outbound: 10, inbound: 10, verdict: 10, full: 10},
	}
	placement := model.PlaceVerifierReading(g, model.VerifierDraft{Targets: []string{target}}, reading)
	if !slices.Equal(placement.Targets, []string{target}) {
		t.Fatalf("targets %v: only the declared target should come in full", placement.Targets)
	}
	reasons := bearingReasons(placement)
	if reasons[closer] != model.VerifierClosesTarget || reasons[superseded] != model.VerifierTargetSupersedes || len(reasons) != 2 {
		t.Fatalf("marked %v: both lifecycle directions should be marked with their reason, unlisted ones too", reasons)
	}
	doubt := map[string]model.Line{}
	for _, line := range placement.Lines.Doubt {
		doubt[line.Entry.ID] = line
	}
	for _, id := range []string{plain, full, outbound, inbound, verdict} {
		if _, ok := doubt[id]; !ok {
			t.Fatalf("doubt %v: %s should be a line", lineIDs(placement.Lines.Doubt), id)
		}
	}
	if !doubt[verdict].Summary {
		t.Fatal("the entry given a verdict should be a line with its summary sentence")
	}
	if !doubt[outbound].Hop || !doubt[inbound].Hop || doubt[plain].Hop || doubt[full].Hop {
		t.Fatal("the entries one reference from the target, and only they, should carry the hop")
	}
}

// The shared order marks what it places as likely bearing; a marked entry
// with a drafter's verdict stays a marked line.
func TestPlaceVerifierReading_MarksWhatTheSharedOrderPlacesAsLikelyBearing(t *testing.T) {
	g := model.NewGraph([]*model.Entry{gap("high"), gap("dismissed"), gap("low")})
	reading := model.VerifierReading{
		Listed: []string{"high", "dismissed", "low"}, Verdicts: []string{"dismissed"},
		Scores:     map[string]uint8{"high": 250, "dismissed": 240, "low": 60},
		Thresholds: model.RelevanceThresholds{Valid: true, SetAside: 0.1, Relevant: 0.9},
	}
	placement := model.PlaceVerifierReading(g, model.VerifierDraft{}, reading)
	reasons := bearingReasons(placement)
	if reasons["high"] != model.VerifierLikelyBearing || reasons["dismissed"] != model.VerifierLikelyBearing {
		t.Fatalf("marked %v: both scores at the relevant threshold should be marked lines", reasons)
	}
	if got := lineIDs(placement.Lines.Doubt); !slices.Equal(got, []string{"low"}) {
		t.Fatalf("doubt %v: the band between the thresholds should stay in doubt", got)
	}
}

// Without any score the verifier judges the reading: its first unscored
// candidates carry the mark, standing by falling similarity, then listed
// order, done entries after all others.
func TestPlaceVerifierReading_MarksTheFirstUnscoredWithoutAJudge(t *testing.T) {
	var entries []*model.Entry
	var listed []string
	for i := range 8 {
		id := fmt.Sprintf("e%d", i)
		entries = append(entries, gap(id))
		listed = append(listed, id)
	}
	entries[0].Kind = model.KindDone
	g := model.NewGraph(entries)
	reading := model.VerifierReading{Listed: listed, Similarity: map[string]float64{"e0": 0.99, "e6": 0.9, "e7": 0.8}}
	placement := model.PlaceVerifierReading(g, model.VerifierDraft{}, reading)
	if got := lineIDs(placement.Lines.Bearing); !slices.Equal(got, []string{"e6", "e7", "e1", "e2", "e3"}) {
		t.Fatalf("marked %v: the first five unscored by similarity, then listed order, done last", got)
	}
	if got := lineIDs(placement.Lines.Doubt); !slices.Equal(got, []string{"e4", "e5", "e0"}) {
		t.Fatalf("doubt %v: the rest in the same order, the done entry last", got)
	}
}

// The verifier's start keeps the limits a graph check keeps, and counts
// what passes them.
func TestPlaceVerifierReading_KeepsTheSharedLimits(t *testing.T) {
	var entries []*model.Entry
	var listed []string
	scores := map[string]uint8{}
	for i := range 450 {
		id := fmt.Sprintf("20260101-%06d-s-tac-r%03d", i, i)
		entries = append(entries, gap(id))
		listed = append(listed, id)
		scores[id] = uint8(100 - i/5)
	}
	for _, widened := range []bool{false, true} {
		placement := model.PlaceVerifierReading(model.NewGraph(entries), model.VerifierDraft{}, model.VerifierReading{Listed: listed, Scores: scores, Widened: widened})
		lines := model.DoubtLines
		if widened {
			lines *= 2
		}
		if len(placement.Lines.Doubt) != lines || len(placement.Lines.Titles) != min(model.TitleLines, 450-lines) || len(placement.Lines.Counted) != max(0, 450-lines-model.TitleLines) {
			t.Fatalf("%d lines, %d titles, %d counted: the limits of a graph check should hold", len(placement.Lines.Doubt), len(placement.Lines.Titles), len(placement.Lines.Counted))
		}
	}
}

// An entry the drafter dismissed is a line with its summary sentence and its
// score wherever the limits would place it, never a title line or a count
// (20261122-101625-d-tac-r0s).
func TestPlaceVerifierReading_ListsEveryDismissedEntryAsALine(t *testing.T) {
	var entries []*model.Entry
	var listed []string
	scores := map[string]uint8{}
	for i := range 450 {
		id := fmt.Sprintf("20260101-%06d-s-tac-r%03d", i, i)
		entries = append(entries, gap(id))
		listed = append(listed, id)
		scores[id] = uint8(100 - i/5)
	}
	titled, counted := listed[model.DoubtLines], listed[449]
	reading := model.VerifierReading{Listed: listed, Scores: scores, Verdicts: []string{titled, counted}}
	placement := model.PlaceVerifierReading(model.NewGraph(entries), model.VerifierDraft{}, reading)
	doubt := map[string]model.Line{}
	for _, line := range placement.Lines.Doubt {
		doubt[line.Entry.ID] = line
	}
	for _, id := range []string{titled, counted} {
		line, ok := doubt[id]
		if !ok || !line.Summary || line.Score == nil {
			t.Fatalf("%s placed %+v: a dismissed entry should be a scored line with its summary sentence", id, line)
		}
		if slices.ContainsFunc(placement.Lines.Titles, func(l model.Line) bool { return l.Entry.ID == id }) || slices.Contains(placement.Lines.Counted, id) {
			t.Fatalf("%s should not also be a title line or counted", id)
		}
	}
}

// A calibrated judge's set-aside threshold sets a candidate aside, but for
// the blind audit share shown among the lines in doubt.
func TestPlaceVerifierReading_SetsAsideBelowTheThreshold(t *testing.T) {
	var entries []*model.Entry
	var listed []string
	scores := map[string]uint8{}
	for i := range 40 {
		id := fmt.Sprintf("low%02d", i)
		entries = append(entries, gap(id))
		listed = append(listed, id)
		scores[id] = 1
	}
	reading := model.VerifierReading{Listed: listed, Scores: scores, Thresholds: model.RelevanceThresholds{Valid: true, SetAside: 0.1, Relevant: 0.9}, Seed: "seed"}
	placement := model.PlaceVerifierReading(model.NewGraph(entries), model.VerifierDraft{}, reading)
	if len(placement.Lines.Audit) == 0 || len(setAsideIDs(placement.Lines.SetAside, model.SetAsideFiltered))+len(placement.Lines.Audit) != 40 {
		t.Fatalf("set aside %v, audit %v: every low score should be set aside but for the audit share", placement.Lines.SetAside, placement.Lines.Audit)
	}
}

// The candidates whose class waits on a score are those without one and
// without a mark of their own.
func TestVerifierUnscored_AsksForScoresWhereTheClassWaitsOnThem(t *testing.T) {
	const target = "20260101-000000-d-tac-tgt"
	closing := &model.Entry{ID: "closer", Type: model.TypeSignal, Kind: model.KindDone, Closes: []string{target}}
	g := model.NewGraph([]*model.Entry{gap(target), closing, gap("scored"), gap("unscored"), gap("dismissed")})
	reading := model.VerifierReading{Listed: []string{"scored", "unscored", "dismissed"}, Verdicts: []string{"dismissed"}, Scores: map[string]uint8{"scored": 100}}
	placement := model.PlaceVerifierReading(g, model.VerifierDraft{Targets: []string{target}}, reading)
	got := model.VerifierUnscored(placement, reading)
	slices.Sort(got)
	if !slices.Equal(got, []string{"dismissed", "unscored"}) {
		t.Fatalf("unscored %v: every candidate without a score or a mark of its own should be asked for one", got)
	}
}

// The ranking sources count the candidates by what ranked them: the judge's
// score, similarity to the draft, or listed order.
func TestVerifierRankingSources_CountsWhatRankedEachEntry(t *testing.T) {
	g := model.NewGraph([]*model.Entry{gap("target"), gap("a"), gap("b"), gap("c"), gap("d")})
	reading := model.VerifierReading{Listed: []string{"a", "b", "c", "d"}, Scores: map[string]uint8{"a": 1, "target": 2}, Similarity: map[string]float64{"b": 0.5}}
	placement := model.PlaceVerifierReading(g, model.VerifierDraft{Targets: []string{"target"}}, reading)
	got := model.VerifierRankingSources(placement, reading)
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
