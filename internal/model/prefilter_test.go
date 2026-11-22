package model_test

import (
	"fmt"
	"slices"
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
	quarter := labelsWith(34*4, 0, 0)
	for i := range quarter {
		quarter[i].Weight = model.WeightImplicit
	}
	if got := model.Calibrate(quarter, nil, "j"); got.Valid {
		t.Fatalf("thresholds %+v: quarter-weight positives weighing 34 fall short of the first stage", got)
	}
	quarter = append(quarter, labelsWith(1, 0, 0)...)
	if got := model.Calibrate(quarter, nil, "j"); !got.Valid || got.Stage != 0.90 {
		t.Fatalf("thresholds %+v: one whole positive more should reach the first stage", got)
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
	if got := model.Calibrate(labelsWith(34, 500, 10), nil, "j"); got.Valid {
		t.Fatalf("thresholds %+v: below the first recall stage the prefilter should only sort", got)
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

// The highest recall stage the positive labels hold sets the threshold: 35
// missless positives hold 0.90, 73 hold 0.95, 189 hold 0.98.
func TestCalibrate_TakesTheHighestStageItsLabelsHold(t *testing.T) {
	for _, tc := range []struct {
		positives int
		stage     float64
	}{{34, 0}, {35, 0.90}, {72, 0.90}, {73, 0.95}, {188, 0.95}, {189, 0.98}} {
		got := model.Calibrate(labelsWith(tc.positives, 0, 0), nil, "j")
		if got.Valid != (tc.stage > 0) || got.Stage != tc.stage {
			t.Errorf("%d positives: thresholds %+v, want stage %.2f", tc.positives, got, tc.stage)
		}
	}
	labels := labelsWith(100, 0, 0)
	for i := range 3 {
		labels[i].Score = uint8(10 + i)
	}
	got := model.Calibrate(labels, nil, "j")
	if got.Stage != 0.95 || got.SetAside != model.ScoreValue(10) {
		t.Fatalf("thresholds %+v: at stage 0.95 a hundred positives tolerate no miss, so the threshold sits at the lowest", got)
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

func TestRelevanceText_KeepsTheWholeEntry(t *testing.T) {
	body := "First line.\n\nA constraint deep in the body."
	text := model.RelevanceText("directive", []string{"a/b", "c"}, "A title", body)
	want := "kind: directive\ntopics: a/b, c\ntitle: A title\n\n" + body
	if text != want {
		t.Fatalf("text %q should hold kind, topics, title and the whole body", text)
	}
}

// A judgement stays with the capture when the draft changes: a score against
// the same draft folds to the highest, one against a revised draft replaces
// the earlier one, and an entry the revised draft did not judge keeps its
// judgement.
func TestReadingKept_KeepsAJudgementAcrossDrafts(t *testing.T) {
	first, revised := model.RelevanceSubjectHash("gap\nthe first draft"), model.RelevanceSubjectHash("gap\nthe revised draft")
	reading := &model.Reading{}
	reading.Fold(model.ReadPayload{Prefilter: &model.PrefilterPayload{Judge: "j", Subject: first, Scores: map[string]uint8{"a": 200, "b": 10}}})
	reading.Fold(model.ReadPayload{Prefilter: &model.PrefilterPayload{Judge: "j", Subject: first, Scores: map[string]uint8{"a": 150}}})
	if kept := reading.Kept("j"); kept["a"] != 200 {
		t.Fatalf("kept %v: a score against the same draft should fold to the highest", kept)
	}
	reading.Fold(model.ReadPayload{Prefilter: &model.PrefilterPayload{Judge: "j", Subject: revised, Scores: map[string]uint8{"a": 20}}})
	kept := reading.Kept("j")
	if kept["a"] != 20 || kept["b"] != 10 {
		t.Fatalf("kept %v: the revised draft's judgement should replace the earlier one and every other stay", kept)
	}
	if stale := reading.Stale("j", revised); len(stale) != 1 || stale["b"] != 10 {
		t.Fatalf("stale %v: only the judgement given against the earlier draft is stale", stale)
	}
	if reading.Kept("other") != nil {
		t.Fatalf("a judge that scored nothing should keep no judgement")
	}
}

// An entry's ranking line is its type, kind, title and summary on one line;
// an untitled one carries its summary alone.
func TestEntryRankingLine(t *testing.T) {
	titled := &model.Entry{Type: model.TypeDecision, Kind: model.KindDirective, Title: "Rank with a judge", Summary: "The deep reading ranks.\nThen it lists."}
	if got := model.EntryRankingLine(titled); got != "[decision/directive] Rank with a judge: The deep reading ranks. Then it lists." {
		t.Fatalf("line %q", got)
	}
	untitled := &model.Entry{Type: model.TypeSignal, Kind: model.KindGap, Summary: "A gap. More."}
	if got := model.EntryRankingLine(untitled); got != "[signal/gap] A gap. More." {
		t.Fatalf("line %q", got)
	}
}

func labelRun(prefix string, n int, score uint8, kind string, source model.ReaderSource) []model.RelevanceLabel {
	labels := make([]model.RelevanceLabel, 0, n)
	for i := range n {
		labels = append(labels, model.RelevanceLabel{Pair: model.NewRelevancePair("a", fmt.Sprintf("%s%04d", prefix, i)), Score: score, Kind: kind, Source: source, Weight: 1})
	}
	return labels
}

// Replayed references are positives only and easier than what a live draft
// needs, so the serve-in-full threshold reads the live labels alone, never
// below the set-aside threshold, and the set-aside threshold takes the lower
// cut of the live labels once they hold a stage of their own.
func TestCalibrate_RestsEachThresholdOnTheLabelsThatProveIt(t *testing.T) {
	spread := labelRun("r", 189, 0, model.LabelPositive, model.SourceReplay)
	for i := range spread {
		spread[i].Score = uint8(3 + i)
	}
	replayed := func(n int, score uint8) []model.RelevanceLabel {
		return labelRun("r", n, score, model.LabelPositive, model.SourceReplay)
	}
	live := func(prefix string, n int, score uint8, kind string) []model.RelevanceLabel {
		return labelRun(prefix, n, score, kind, model.SourceLive)
	}
	cases := []struct {
		name     string
		labels   []model.RelevanceLabel
		setAside uint8
		// relevant is the serve-in-full threshold, 0 for none.
		relevant uint8
		// live reports a set-aside threshold cut by the live labels alone.
		live bool
	}{
		{"replayed references alone serve nothing in full", spread, 3, 0, false},
		{"the serve-in-full threshold reads the live labels",
			slices.Concat(replayed(200, 3), live("p", 40, 230, model.LabelPositive), live("n", 40, 100, model.LabelNegative)), 3, 230, false},
		{"live labels short of the precision bound serve nothing in full",
			slices.Concat(replayed(200, 3), live("p", 3, 230, model.LabelPositive)), 3, 0, false},
		{"the serve-in-full threshold stays at or above the set-aside threshold",
			slices.Concat(live("p", 398, 200, model.LabelPositive), live("q", 2, 50, model.LabelPositive)), 200, 200, false},
		{"the lower cut of live labels holding a stage sets aside",
			slices.Concat(replayed(400, 51), live("p", 38, 60, model.LabelPositive), live("q", 2, 20, model.LabelPositive), live("n", 40, 40, model.LabelNegative)), 20, 60, true},
		{"below a live stage the cut over all labels stands",
			slices.Concat(replayed(400, 51), live("p", 2, 20, model.LabelPositive)), 51, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := model.Calibrate(tc.labels, nil, "j")
			if !got.Valid || got.SetAside != model.ScoreValue(tc.setAside) || got.SetAsideLive != tc.live {
				t.Errorf("thresholds %+v: the set-aside threshold should rest on the labels that prove it", got)
			}
			if tc.relevant == 0 && (got.Relevant <= 1 || got.ServesInFull()) {
				t.Errorf("thresholds %+v: no score should be served in full", got)
			}
			if tc.relevant != 0 && (got.Relevant != model.ScoreValue(tc.relevant) || !got.ServesInFull()) {
				t.Errorf("thresholds %+v: the serve-in-full threshold should rest on the live labels", got)
			}
		})
	}
}

// A candidate below the set-aside threshold is set aside whatever the
// relevant threshold says.
func TestPlaceLines_SetsAsideBeforeItMarks(t *testing.T) {
	thresholds := model.RelevanceThresholds{Valid: true, SetAside: 0.2, Relevant: 0.012}
	cases := []struct {
		name     string
		score    uint8
		setAside bool
	}{
		{"below both thresholds", 3, true},
		{"between the thresholds", 25, true},
		{"at the set-aside threshold", 51, false},
		{"above both thresholds", 250, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			placed := model.PlaceLines(model.LineInput{Candidates: []model.GatherCandidate{{Entry: &model.Entry{ID: "c"}}}, Scores: map[string]uint8{"c": tc.score}, Thresholds: thresholds})
			if got := len(placed.SetAside) == 1; got != tc.setAside {
				t.Fatalf("placed %+v: a score below the set-aside threshold, and only that, should be set aside", placed)
			}
			if got := len(placed.Bearing) == 1; got == tc.setAside {
				t.Fatalf("placed %+v: a score not set aside should be marked as likely bearing", placed)
			}
		})
	}
}

// A done candidate with a score ranks by it; among equal scores and among
// the unscored it ranks last, the rest keep the order given.
func TestPlaceLines_RanksDoneByScoreAndLastOnATie(t *testing.T) {
	entry := func(id string, kind model.Kind) model.GatherCandidate {
		return model.GatherCandidate{Entry: &model.Entry{ID: id, Type: model.TypeSignal, Kind: kind}}
	}
	done, gap := entry("20260101-000000-s-tac-don", model.KindDone), entry("20260101-000001-s-tac-gap", model.KindGap)
	tiedDone, tiedGap := entry("20260101-000002-s-tac-tdn", model.KindDone), entry("20260101-000003-s-tac-tgp", model.KindGap)
	openDone, unscored := entry("20260101-000004-s-tac-odn", model.KindDone), entry("20260101-000005-s-tac-uns", model.KindGap)
	scores := map[string]uint8{done.Entry.ID: 230, gap.Entry.ID: 100, tiedDone.Entry.ID: 180, tiedGap.Entry.ID: 180}

	placed := model.PlaceLines(model.LineInput{Candidates: []model.GatherCandidate{openDone, unscored, tiedDone, tiedGap, gap, done}, Scores: scores})
	var got []string
	for _, line := range slices.Concat(placed.Bearing, placed.Doubt) {
		got = append(got, line.Entry.ID)
	}
	want := []string{done.Entry.ID, tiedGap.Entry.ID, tiedDone.Entry.ID, gap.Entry.ID, unscored.Entry.ID, openDone.Entry.ID}
	if !slices.Equal(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
}
