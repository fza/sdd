package model_test

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func laneTopics(t *testing.T, labels ...string) []model.TopicPath {
	t.Helper()
	var paths []model.TopicPath
	for _, label := range labels {
		path, err := model.ParseTopicPath(label)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	return paths
}

func TestVerifierStructural_NamesTheEntriesAlwaysInspected(t *testing.T) {
	const (
		target    = "20260101-000000-d-tac-tgt"
		outbound  = "20260101-000001-s-tac-out"
		inbound   = "20260101-000002-s-tac-inb"
		closer    = "20260101-000003-d-tac-cls"
		sameKind  = "20260101-000004-s-tac-sam"
		wideKind  = "20260101-000005-s-tac-wid"
		otherKind = "20260101-000006-d-tac-oth"
		full      = "20260101-000007-s-tac-ful"
		verdict   = "20260101-000008-s-tac-ver"
		plain     = "20260101-000009-s-tac-pln"
	)
	entries := []*model.Entry{
		{ID: target, Type: model.TypeDecision, Kind: model.KindDirective, Refs: []model.Ref{{ID: outbound}}},
		{ID: outbound, Type: model.TypeSignal, Kind: model.KindGap},
		{ID: inbound, Type: model.TypeSignal, Kind: model.KindGap, Refs: []model.Ref{{ID: target}}},
		{ID: closer, Type: model.TypeDecision, Kind: model.KindDone, Closes: []string{target}},
		{ID: sameKind, Type: model.TypeSignal, Kind: model.KindGap, Topics: laneTopics(t, "grounding/verifier/lanes")},
		{ID: wideKind, Type: model.TypeSignal, Kind: model.KindGap, Topics: laneTopics(t, "grounding")},
		{ID: otherKind, Type: model.TypeDecision, Kind: model.KindDirective, Topics: laneTopics(t, "grounding/verifier")},
		{ID: full, Type: model.TypeSignal, Kind: model.KindGap},
		{ID: verdict, Type: model.TypeSignal, Kind: model.KindGap},
		{ID: plain, Type: model.TypeSignal, Kind: model.KindGap},
	}
	g := model.NewGraph(entries)
	listed := []string{outbound, inbound, closer, sameKind, wideKind, otherKind, full, verdict, plain}
	draft := model.VerifierDraft{Kind: model.KindGap, Topics: laneTopics(t, "grounding", "grounding/verifier"), Targets: []string{target}}

	got := slices.Sorted(maps.Keys(model.VerifierStructural(g, draft, listed, []string{full}, []string{verdict})))
	want := []string{target, outbound, inbound, closer, sameKind, full, verdict}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("structural = %v\nwant %v: targets, one hop either way, full reads, verdicts and same-kind entries on the narrowest topic", got, want)
	}
}

func TestSplitVerifierLanes(t *testing.T) {
	valid := model.RelevanceThresholds{Valid: true, SetAside: 0.4, Relevant: 0.9}
	threshold := valid.SetAside
	listed := []string{"a-structural", "b-high", "c-low", "d-unscored", "e-at", "f-low"}
	structural := map[string]bool{"a-structural": true}
	scores := map[string]uint8{"a-structural": 0, "b-high": 200, "c-low": 10, "e-at": model.ScoreByte(0.4), "f-low": 20}
	cases := []struct {
		name        string
		thresholds  model.RelevanceThresholds
		threshold   float64
		wantInspect []string
		wantHidden  []string
	}{
		{"invalid calibration inspects everything", model.RelevanceThresholds{}, 0.9, listed, nil},
		{"valid calibration hides what scores below the threshold", valid, threshold, []string{"a-structural", "b-high", "d-unscored", "e-at"}, []string{"c-low", "f-low"}},
		{"a higher threshold hides more", valid, 0.9, []string{"a-structural", "d-unscored"}, []string{"b-high", "c-low", "e-at", "f-low"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lanes := model.SplitVerifierLanes(listed, structural, scores, c.thresholds, c.threshold, "seed")
			inspect := slices.DeleteFunc(slices.Clone(lanes.Inspect), func(id string) bool { return slices.Contains(lanes.Audit, id) })
			hidden := append(slices.Clone(lanes.Hidden), lanes.Audit...)
			slices.SortStableFunc(hidden, func(a, b string) int { return slices.Index(listed, a) - slices.Index(listed, b) })
			if !slices.Equal(inspect, c.wantInspect) || !slices.Equal(hidden, c.wantHidden) {
				t.Fatalf("inspect %v, hidden %v (audit %v); want inspect %v, hidden %v", lanes.Inspect, lanes.Hidden, lanes.Audit, c.wantInspect, c.wantHidden)
			}
			if len(lanes.Inspect)+len(lanes.Hidden) != len(listed) {
				t.Fatalf("inspect %v and hidden %v should hold every listed entry once", lanes.Inspect, lanes.Hidden)
			}
		})
	}
}

func TestSplitVerifierLanes_ShowsTheAuditShareOfTheHiddenBlind(t *testing.T) {
	var listed []string
	scores := map[string]uint8{}
	for i := range 100 {
		id := fmt.Sprintf("20260101-%06d-s-tac-low", i)
		listed = append(listed, id)
		scores[id] = 1
	}
	listed = append(listed, "20260102-000000-s-tac-hig")
	scores["20260102-000000-s-tac-hig"] = 250
	thresholds := model.RelevanceThresholds{Valid: true, SetAside: 0.5, Relevant: 0.9}

	lanes := model.SplitVerifierLanes(listed, nil, scores, thresholds, thresholds.SetAside, "seed")
	if len(lanes.Audit) != 3 || len(lanes.Hidden) != 97 || len(lanes.Inspect) != 4 {
		t.Fatalf("lanes %d inspected, %d hidden, %d audited: ceil(3%%) of the 100 below the threshold should return blind", len(lanes.Inspect), len(lanes.Hidden), len(lanes.Audit))
	}
	for _, id := range lanes.Audit {
		if !slices.Contains(lanes.Inspect, id) || slices.Contains(lanes.Hidden, id) {
			t.Fatalf("audit sample %s should be inspected and not hidden", id)
		}
	}
	again := model.SplitVerifierLanes(listed, nil, scores, thresholds, thresholds.SetAside, "seed")
	if !slices.Equal(again.Inspect, lanes.Inspect) {
		t.Fatal("one seed should place the same samples at the same positions")
	}
}
