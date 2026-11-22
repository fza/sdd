package model_test

import (
	"fmt"
	"slices"
	"testing"

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
	report := model.NewRelevanceLedger(batch).Calibration("j", false)
	if !report.Thresholds.Valid || report.Positive != 189 || report.Negative != 16 {
		t.Fatalf("report %+v should count the labels and hold thresholds", report)
	}
	if report.PassedInFull != 205 || report.Referenced != 189 {
		t.Fatalf("report %+v should count the pairs passed in full", report)
	}
	if report.DominantCode() != model.VerdictTopicOnly || len(report.SetAside) != 2 || report.SetAside[1].Code != model.VerdictTermClash {
		t.Fatalf("set aside %+v should run by count, the dominant code first", report.SetAside)
	}
	if report.Unscored != 0 || model.NewRelevanceLedger(batch).Calibration("new judge", false).Unscored != 205 {
		t.Fatal("a new judge should report every labelled pair as unscored")
	}
}

func TestCalibration_CountsBothSourcesAndExcludesReplayOnRequest(t *testing.T) {
	var batch model.RelevanceBatch
	add := func(i int, score uint8, verdict model.ReaderVerdict) {
		pair := model.NewRelevancePair(fmt.Sprintf("20260101-%06d-s-tac-can", i), "20260601-000000-d-tac-sub")
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: verdict})
		batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: "j", Score: score}})
	}
	for i := range 150 {
		add(i, 220, model.ReaderVerdict{Code: model.ReaderReferenced})
	}
	for i := range 60 {
		add(1000+i, 200, model.ReaderVerdict{Code: model.ReaderReferenced, Source: model.SourceReplay})
	}
	ledger := model.NewRelevanceLedger(batch)
	both := ledger.Calibration("j", false)
	if both.Thresholds.Stage != model.RecallFloor || both.Positive != 210 || both.Live != 150 || both.Replay != 60 || both.ReplayExcluded {
		t.Fatalf("report %+v should calibrate on both sources and count each", both)
	}
	if both.Thresholds.SetAside != model.ScoreValue(200) || both.Thresholds.Relevant != model.ScoreValue(220) {
		t.Fatalf("thresholds %+v should set aside below the lowest reference of either source and serve in full from the live ones", both.Thresholds)
	}
	live := ledger.Calibration("j", true)
	if live.Thresholds.Stage != 0.95 || live.Positive != 150 || live.Live != 150 || live.Replay != 60 || !live.ReplayExcluded {
		t.Fatalf("report %+v should read the live labels alone and still count the replayed ones", live)
	}
	if got := model.Calibrate(ledger.Labels("j"), nil, "j"); got != both.Thresholds {
		t.Fatalf("Calibrate over every label = %+v, want the report's thresholds", got)
	}
}

func TestCalibration_CountsOpenedHiddenEntries(t *testing.T) {
	var batch model.RelevanceBatch
	add := func(i int, verdict model.ReaderVerdict) {
		pair := model.NewRelevancePair(fmt.Sprintf("20260101-%06d-s-tac-can", i), "20260601-000000-d-tac-sub")
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: verdict})
		batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: "j", Score: 10}})
	}
	topicOnly, _ := model.VerdictReasonByte(model.VerdictTopicOnly)
	for i := range 60 {
		add(i, model.ReaderVerdict{Code: model.ReaderDismissed, Reason: topicOnly, Audit: true})
	}
	for i := range 5 {
		add(100+i, model.ReaderVerdict{OpenedHidden: true})
	}
	add(200, model.ReaderVerdict{Code: model.ReaderReferenced, Audit: true})
	report := model.NewRelevanceLedger(batch).Calibration("j", false)
	if report.OpenedHidden != 5 {
		t.Fatalf("opened hidden = %d: each opening without a finding should count", report.OpenedHidden)
	}
}

func TestCalibration_ReportsTheFlipRate(t *testing.T) {
	var batch model.RelevanceBatch
	for i := range 4 {
		pair := model.NewRelevancePair(fmt.Sprintf("20260101-%06d-s-tac-can", i), "20260601-000000-d-tac-sub")
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderImplicit, Flipped: i == 0}})
		batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: "j", Score: 10}})
	}
	batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: model.NewRelevancePair("20260101-000009-s-tac-ref", "20260601-000000-d-tac-sub"), Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}})
	ledger := model.NewRelevanceLedger(batch)
	report := ledger.Calibration("j", false)
	if report.Implicit != 4 || report.Flipped != 1 || report.FlipRate() != 0.25 {
		t.Fatalf("implicit %d, flipped %d, rate %.2f: one flip of four implicit dismissals", report.Implicit, report.Flipped, report.FlipRate())
	}
	if report.Positive != 1 || report.Negative != 3 {
		t.Fatalf("labels %d positive, %d negative: the flip should label its pair positive", report.Positive, report.Negative)
	}
	for _, label := range ledger.Labels("j") {
		if label.Weight != model.WeightImplicit {
			t.Fatalf("label %+v should carry the implicit weight", label)
		}
	}
	if (model.CalibrationReport{}).FlipRate() != 0 {
		t.Fatal("no implicit dismissal should report a zero rate")
	}
}

// A judge's ranking recall is the weighted share of the positive labels it
// scores at or above RankingRecallLine. Measured from RankingRecallMinimum
// positives on, a share below RankingRecallBar marks the judge weak.
func TestRankingHealth(t *testing.T) {
	labels := func(high, low int) []model.RelevanceLabel {
		var out []model.RelevanceLabel
		for i := range high + low {
			score := uint8(250)
			if i >= high {
				score = 10
			}
			out = append(out, model.RelevanceLabel{Kind: model.LabelPositive, Score: score, Weight: model.WeightReferenced})
		}
		return append(out, model.RelevanceLabel{Kind: model.LabelNegative, Score: 250, Weight: model.WeightDismissed})
	}
	few := model.JudgeRankingHealth(labels(0, model.RankingRecallMinimum-1))
	if few.Measured || few.Weak() {
		t.Fatalf("health %+v: fewer positives than the minimum measure nothing", few)
	}
	weak := model.JudgeRankingHealth(labels(17, 3))
	if !weak.Measured || weak.Positives != 20 || weak.Recall != 0.85 || !weak.Weak() {
		t.Fatalf("health %+v: 17 of 20 positives at the line is below the bar", weak)
	}
	sound := model.JudgeRankingHealth(labels(19, 1))
	if !sound.Measured || sound.Recall != 0.95 || sound.Weak() {
		t.Fatalf("health %+v: 19 of 20 positives at the line clears the bar", sound)
	}
}
