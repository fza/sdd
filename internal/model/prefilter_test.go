package model_test

import (
	"fmt"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// The label counts D33 names: the recall bound reaches the floor at 189
// referenced labels with no miss, 280 with one, 361 with two.
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

func labelsWith(positives int, positiveScore uint8, negatives int, negativeScore uint8) []model.RelevanceLabel {
	var labels []model.RelevanceLabel
	for i := range positives {
		labels = append(labels, model.RelevanceLabel{Pair: model.NewRelevancePair("a", fmt.Sprintf("p%04d", i)), Score: positiveScore, Kind: model.LabelPositive})
	}
	for i := range negatives {
		labels = append(labels, model.RelevanceLabel{Pair: model.NewRelevancePair("a", fmt.Sprintf("n%04d", i)), Score: negativeScore, Kind: model.LabelNegative})
	}
	return labels
}

func TestCalibrate(t *testing.T) {
	if got := model.Calibrate(labelsWith(188, 200, 500, 10), nil, "j"); got.Valid {
		t.Fatalf("thresholds %+v: below the recall bound the prefilter should only sort", got)
	}
	got := model.Calibrate(labelsWith(189, 200, 500, 10), nil, "j")
	if !got.Valid || got.SetAside != model.ScoreValue(200) {
		t.Fatalf("thresholds %+v: at the bound the set-aside threshold should sit at the lowest referenced score", got)
	}
	if got.Relevant != model.ScoreValue(200) {
		t.Fatalf("thresholds %+v: the relevant threshold should sit where referenced pairs dominate", got)
	}
	stepped := model.Calibrate(labelsWith(189, 200, 500, 10), []model.DriftStep{{Judge: "j", SetAside: 50}, {Judge: "other", SetAside: 1}}, "j")
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
	text := model.RelevanceText("directive", []string{"a/b", "c"}, "A title", body)
	want := "kind: directive\ntopics: a/b, c\ntitle: A title\n\n" + body
	if text != want {
		t.Fatalf("text %q should hold kind, topics, title and the whole body", text)
	}
}
