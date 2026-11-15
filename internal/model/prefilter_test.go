package model_test

import (
	"fmt"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// The recall bound reaches the floor at 189 referenced labels with no miss,
// 280 with one, 361 with two.
func TestWilsonLower_ReachesTheRecallFloorAtTheNamedCounts(t *testing.T) {
	for _, tc := range []struct{ n, misses int }{{189, 0}, {280, 1}, {361, 2}} {
		if got := model.WilsonLower(tc.n-tc.misses, tc.n); got < model.RecallFloor {
			t.Errorf("%d labels with %d misses: bound %.5f should reach the floor", tc.n, tc.misses, got)
		}
		if got := model.WilsonLower(tc.n-1-tc.misses, tc.n-1); got >= model.RecallFloor {
			t.Errorf("%d labels with %d misses: bound %.5f should stay below the floor", tc.n-1, tc.misses, got)
		}
	}
}

func labelsWith(positives int, negatives int, negativeScore uint8) []model.RelevanceLabel {
	var labels []model.RelevanceLabel
	for i := range positives {
		labels = append(labels, model.RelevanceLabel{Pair: model.NewRelevancePair("a", fmt.Sprintf("p%04d", i)), Score: 200, Kind: model.LabelPositive, Weight: 1})
	}
	for i := range negatives {
		labels = append(labels, model.RelevanceLabel{Pair: model.NewRelevancePair("a", fmt.Sprintf("n%04d", i)), Score: negativeScore, Kind: model.LabelNegative, Weight: 1})
	}
	return labels
}

func TestWilsonLowerWeighted_MatchesTheCountedBoundAtUnitWeights(t *testing.T) {
	for _, tc := range []struct{ successes, n int }{{0, 0}, {189, 189}, {279, 280}, {5, 10}} {
		if got, want := model.WilsonLowerWeighted(float64(tc.successes), float64(tc.n)), model.WilsonLower(tc.successes, tc.n); got != want {
			t.Errorf("%d of %d: weighted bound %.6f should equal the counted bound %.6f", tc.successes, tc.n, got, want)
		}
	}
	if model.WilsonLowerWeighted(189*0.25, 189*0.25) >= model.RecallFloor {
		t.Error("189 positives at a quarter weight should not reach the recall floor")
	}
}

func TestLabelWeight_FollowsTheReaderState(t *testing.T) {
	cases := map[string]struct {
		verdict model.ReaderVerdict
		want    float64
	}{
		"referenced":         {model.ReaderVerdict{Code: model.ReaderReferenced}, model.WeightReferenced},
		"opened":             {model.ReaderVerdict{Code: model.ReaderOpened, Reason: 1}, model.WeightOpened},
		"explicit dismissal": {model.ReaderVerdict{Code: model.ReaderDismissed, Reason: 1}, model.WeightDismissed},
		"implicit dismissal": {model.ReaderVerdict{Code: model.ReaderImplicit}, model.WeightImplicit},
		"flipped":            {model.ReaderVerdict{Code: model.ReaderImplicit, Flipped: true}, model.WeightImplicit},
		"none":               {model.ReaderVerdict{OpenedHidden: true}, 0},
	}
	for name, tc := range cases {
		if got := model.LabelWeight(tc.verdict); got != tc.want {
			t.Errorf("%s: weight %.2f, want %.2f", name, got, tc.want)
		}
	}
	if model.WeightReferenced != 1 || model.WeightOpened != 0.5 || model.WeightDismissed != 1 || model.WeightImplicit != 0.25 {
		t.Error("the weights should be 1 referenced, 0.5 opened, 1 explicit dismissal, 0.25 implicit dismissal")
	}
}

// Calibration counts each label by its weight: positives a flip earned at a
// quarter weight need four times as many labels to reach the recall bound,
// and implicit negatives weigh a quarter against the relevant threshold.
func TestCalibrate_CountsLabelsByWeight(t *testing.T) {
	quarter := labelsWith(188*4, 0, 0)
	for i := range quarter {
		quarter[i].Weight = model.WeightImplicit
	}
	if got := model.Calibrate(quarter, nil, "j"); got.Valid {
		t.Fatalf("thresholds %+v: quarter-weight positives weighing 188 fall short of the bound", got)
	}
	quarter = append(quarter, labelsWith(1, 0, 0)...)
	if got := model.Calibrate(quarter, nil, "j"); !got.Valid {
		t.Fatalf("thresholds %+v: one whole positive more should reach the bound", got)
	}

	judged := labelsWith(189, 0, 0)
	for i := range 300 {
		judged = append(judged, model.RelevanceLabel{Pair: model.NewRelevancePair("a", fmt.Sprintf("i%04d", i)), Score: 200, Kind: model.LabelNegative, Weight: model.WeightImplicit})
	}
	if got := model.Calibrate(judged, nil, "j"); got.Relevant != model.ScoreValue(200) {
		t.Fatalf("thresholds %+v: 300 implicit negatives weigh 75 against 189 referenced pairs, which still dominate", got)
	}
}

func TestCalibrate(t *testing.T) {
	if got := model.Calibrate(labelsWith(188, 500, 10), nil, "j"); got.Valid {
		t.Fatalf("thresholds %+v: below the recall bound the prefilter should only sort", got)
	}
	got := model.Calibrate(labelsWith(189, 500, 10), nil, "j")
	if !got.Valid || got.SetAside != model.ScoreValue(200) {
		t.Fatalf("thresholds %+v: at the bound the set-aside threshold should sit at the lowest referenced score", got)
	}
	if got.Relevant != model.ScoreValue(200) {
		t.Fatalf("thresholds %+v: the relevant threshold should sit where referenced pairs dominate", got)
	}
	stepped := model.Calibrate(labelsWith(189, 500, 10), []model.DriftStep{{Judge: "j", SetAside: 50}, {Judge: "other", SetAside: 1}}, "j")
	if stepped.SetAside != model.ScoreValue(50) {
		t.Fatalf("thresholds %+v: the judge's drift step should cap the set-aside threshold", stepped)
	}
}

func TestLabelKind_FollowsTheReasonCodes(t *testing.T) {
	reason := func(code string) uint8 {
		b, err := model.VerdictReasonByte(code)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	cases := map[string]struct {
		verdict model.ReaderVerdict
		want    string
	}{
		"referenced":  {model.ReaderVerdict{Code: model.ReaderReferenced}, model.LabelPositive},
		"topic-only":  {model.ReaderVerdict{Code: model.ReaderDismissed, Reason: reason(model.VerdictTopicOnly)}, model.LabelNegative},
		"other-scope": {model.ReaderVerdict{Code: model.ReaderOpened, Reason: reason(model.VerdictOtherScope)}, model.LabelNegative},
		"term-clash":  {model.ReaderVerdict{Code: model.ReaderDismissed, Reason: reason(model.VerdictTermClash)}, model.LabelNegative},
		"covered":     {model.ReaderVerdict{Code: model.ReaderOpened, Reason: reason(model.VerdictCovered)}, model.LabelExcluded},
		"meta":        {model.ReaderVerdict{Code: model.ReaderDismissed, Reason: reason(model.VerdictMeta)}, model.LabelExcluded},
		"other":       {model.ReaderVerdict{Code: model.ReaderDismissed, Reason: reason(model.VerdictOther)}, model.LabelCounted},
		"unknown":     {model.ReaderVerdict{Code: model.ReaderDismissed, Reason: 99}, model.LabelExcluded},
		"implicit":    {model.ReaderVerdict{Code: model.ReaderImplicit}, model.LabelNegative},
		"flipped":     {model.ReaderVerdict{Code: model.ReaderImplicit, Flipped: true}, model.LabelPositive},
		"opened only": {model.ReaderVerdict{Code: model.ReaderOpened}, model.LabelExcluded},
	}
	for name, tc := range cases {
		if got := model.LabelKind(tc.verdict); got != tc.want {
			t.Errorf("%s: label kind %s, want %s", name, got, tc.want)
		}
	}
}

func TestPlacePrefiltered(t *testing.T) {
	entry := func(id string) model.GatherCandidate { return model.GatherCandidate{Entry: &model.Entry{ID: id}} }
	candidates := []model.GatherCandidate{entry("low"), entry("mid"), entry("high"), entry("unscored")}
	scores := map[string]uint8{"low": 5, "mid": 100, "high": 250}

	sorted := model.PlacePrefiltered(candidates, scores, model.RelevanceThresholds{SetAside: 0.5, Relevant: 0.9})
	if len(sorted.Relevant) != 0 || len(sorted.SetAside) != 0 || len(sorted.ToJudge) != 4 {
		t.Fatalf("placed %+v: uncalibrated scores should set nothing aside and serve nothing", sorted)
	}
	if sorted.ToJudge[0].Entry.ID != "high" || sorted.ToJudge[2].Entry.ID != "low" || sorted.ToJudge[3].Entry.ID != "unscored" {
		t.Fatalf("to judge %v should run by falling score, unscored last", ids(sorted.ToJudge))
	}

	calibrated := model.PlacePrefiltered(candidates, scores, model.RelevanceThresholds{Valid: true, SetAside: 0.1, Relevant: 0.9})
	if len(calibrated.Relevant) != 1 || calibrated.Relevant[0].Entry.ID != "high" {
		t.Fatalf("relevant %v should hold the high score", ids(calibrated.Relevant))
	}
	if len(calibrated.SetAside) != 1 || calibrated.SetAside[0] != "low" {
		t.Fatalf("set aside %v should hold the low score", calibrated.SetAside)
	}
	if got := ids(calibrated.ToJudge); len(got) != 2 || got[0] != "mid" || got[1] != "unscored" {
		t.Fatalf("to judge %v should keep the band and the unscored", got)
	}
}

func ids(candidates []model.GatherCandidate) []string {
	var out []string
	for _, candidate := range candidates {
		out = append(out, candidate.Entry.ID)
	}
	return out
}

func TestRelevanceText_KeepsTheWholeEntry(t *testing.T) {
	body := "First line.\n\nA constraint deep in the body."
	text := model.RelevanceText("directive", []string{"a/b", "c"}, body)
	want := "kind: directive\ntopics: a/b, c\n\n" + body
	if text != want {
		t.Fatalf("text %q should hold kind, topics and the whole body", text)
	}
}
