package model_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

func TestAuditSample_ShowsAStableShareBlind(t *testing.T) {
	var setAside []string
	for i := range 100 {
		setAside = append(setAside, fmt.Sprintf("20260101-%06d-s-tac-aaa", i))
	}
	audit, kept := model.AuditSample(setAside, "seed")
	if len(audit) != 3 || len(kept) != 97 {
		t.Fatalf("%d audited, %d kept: a 3%% share should be shown blind", len(audit), len(kept))
	}
	repeat, _ := model.AuditSample(setAside, "seed")
	if !slices.Equal(audit, repeat) {
		t.Fatal("the same reading should audit the same entries on every gather")
	}
	for _, id := range audit {
		if slices.Contains(kept, id) {
			t.Fatalf("%s is both audited and kept", id)
		}
	}
	few, _ := model.AuditSample(setAside[:5], "seed")
	if len(few) != 1 {
		t.Fatalf("%d audited of five: any set-aside list should yield at least one sample", len(few))
	}
	if none, _ := model.AuditSample(nil, "seed"); none != nil {
		t.Fatal("nothing set aside should audit nothing")
	}
}

func TestInsertBlind_HidesTheSampleScore(t *testing.T) {
	score := 0.01
	toJudge := []model.GatherCandidate{{Entry: &model.Entry{ID: "a"}}, {Entry: &model.Entry{ID: "b"}}}
	got := model.InsertBlind(toJudge, []model.GatherCandidate{{Entry: &model.Entry{ID: "s"}, Score: &score}}, "seed")
	if len(got) != 3 {
		t.Fatalf("%d lines: the sample should join the lines to judge", len(got))
	}
	for _, candidate := range got {
		if candidate.Entry.ID == "s" && candidate.Score != nil {
			t.Fatal("an audit sample should carry no score")
		}
	}
}

// driftLabels returns a calibrated label set plus count audit labels by
// subject entries after base, misses of them referenced.
func driftLabels(count, misses int) []model.RelevanceLabel {
	labels := labelsWith(189, 0, 0)
	for i := range count {
		kind := model.LabelNegative
		if i < misses {
			kind = model.LabelPositive
		}
		labels = append(labels, model.RelevanceLabel{Pair: model.NewRelevancePair(fmt.Sprintf("20260101-%06d-s-tac-aud", i), fmt.Sprintf("20260601-%06d-d-tac-sub", i)), Score: 5, Kind: kind, Audit: true})
	}
	return labels
}

func TestDriftStepDue(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		labels []model.RelevanceLabel
		steps  []model.DriftStep
		due    bool
	}{
		{"too few audit labels", driftLabels(49, 10), nil, false},
		{"misses at the limit", driftLabels(100, 2), nil, false},
		{"misses above the limit", driftLabels(100, 3), nil, true},
		{"a step today already", driftLabels(100, 3), []model.DriftStep{{Judge: "j", At: now.Add(-23 * time.Hour), SetAside: 150}}, false},
		{"a step yesterday", driftLabels(100, 3), []model.DriftStep{{Judge: "j", At: now.Add(-25 * time.Hour), SetAside: 150}}, true},
		{"another judge's step", driftLabels(100, 3), []model.DriftStep{{Judge: "k", At: now, SetAside: 1}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step, due := model.DriftStepDue(tc.labels, tc.steps, "j", now)
			if due != tc.due {
				t.Fatalf("due = %v", due)
			}
			if !due {
				return
			}
			current := model.Calibrate(tc.labels, tc.steps, "j")
			if step.Judge != "j" || !step.At.Equal(now) || model.ScoreValue(step.SetAside) >= current.SetAside {
				t.Fatalf("step %+v should lower the judge's set-aside threshold below %.3f", step, current.SetAside)
			}
			after := model.Calibrate(tc.labels, append(tc.steps, step), "j")
			if after.SetAside > current.SetAside {
				t.Fatal("a step should never raise the threshold")
			}
		})
	}
	window := driftLabels(300, 0)
	for i := range 10 {
		window[189+i].Kind = model.LabelPositive
	}
	if _, due := model.DriftStepDue(window, nil, "j", now); due {
		t.Fatal("misses older than the latest 200 audit labels should not count")
	}
}

func TestCalibration_ReportsPrecisionPerCode(t *testing.T) {
	topicOnly, _ := model.VerdictReasonByte(model.VerdictTopicOnly)
	termClash, _ := model.VerdictReasonByte(model.VerdictTermClash)
	var batch model.RelevanceBatch
	add := func(i int, score uint8, verdict model.ReaderVerdict) {
		pair := model.NewRelevancePair(fmt.Sprintf("20260101-%06d-s-tac-can", i), "20260601-000000-d-tac-sub")
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: verdict})
		batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: "j", Score: score}})
	}
	for i := range 189 {
		add(i, 220, model.ReaderVerdict{Code: model.ReaderReferenced})
	}
	for i := range 12 {
		add(1000+i, 230, model.ReaderVerdict{Code: model.ReaderOpened, Reason: topicOnly})
	}
	for i := range 4 {
		add(2000+i, 230, model.ReaderVerdict{Code: model.ReaderDismissed, Reason: termClash})
	}
	report := model.NewRelevanceLedger(batch).Calibration("j")
	if !report.Thresholds.Valid || report.Positive != 189 || report.Negative != 16 {
		t.Fatalf("report %+v should count the labels and hold thresholds", report)
	}
	if report.PassedInFull != 205 || report.Referenced != 189 {
		t.Fatalf("report %+v should count the pairs passed in full", report)
	}
	if report.DominantCode() != model.VerdictTopicOnly || len(report.SetAside) != 2 || report.SetAside[1].Code != model.VerdictTermClash {
		t.Fatalf("set aside %+v should run by count, the dominant code first", report.SetAside)
	}
	if report.Unscored != 0 || model.NewRelevanceLedger(batch).Calibration("new judge").Unscored != 205 {
		t.Fatal("a new judge should report every labelled pair as unscored")
	}
}
