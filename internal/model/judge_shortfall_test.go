package model_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// A check short of its minimum names one cause, the first that holds of a
// failed call, a running calibration, a missing recall stage and a slow
// judge; at the minimum, or with the whole shorter list judged, it is not
// short.
func TestNewJudgeShortfall_NamesOneCause(t *testing.T) {
	cases := []struct {
		name  string
		state model.JudgeState
		want  model.JudgeShortfallCause
	}{
		{"failed", model.JudgeState{Failures: []string{"connection refused"}, Calibrating: true, Stage: 0.9}, model.ShortfallFailed},
		{"calibrating", model.JudgeState{Calibrating: true, CalibrationScored: 3, CalibrationOf: 9}, model.ShortfallCalibrating},
		{"no stage", model.JudgeState{}, model.ShortfallUncalibrated},
		{"a lower stage than its pairs give", model.JudgeState{Stage: 0.9, Reachable: 0.95, Unscored: 40}, model.ShortfallUncalibrated},
		{"slow", model.JudgeState{Stage: 0.95, Reachable: 0.95}, model.ShortfallSlow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shortfall, short := model.NewJudgeShortfall(12, 80, 50, time.Minute, tc.state)
			if !short || shortfall.Cause != tc.want {
				t.Fatalf("shortfall %+v, %v: want cause %s", shortfall, short, tc.want)
			}
			if shortfall.Missing() != 38 {
				t.Errorf("missing %d: the minimum less the judged", shortfall.Missing())
			}
		})
	}
	if _, short := model.NewJudgeShortfall(50, 80, 50, time.Minute, model.JudgeState{}); short {
		t.Error("a check at its minimum is not short")
	}
	if _, short := model.NewJudgeShortfall(20, 20, 50, time.Minute, model.JudgeState{}); short {
		t.Error("a list shorter than the minimum and judged whole is not short")
	}
}

// The judge's time limit reads 1 minute unset, none at 0 and 30 seconds for
// any shorter positive value; the minimum is a count, 50 unset.
func TestVerifyConfig_JudgeLimits(t *testing.T) {
	for _, tc := range []struct {
		value model.JudgeDuration
		want  time.Duration
	}{
		{"", time.Minute},
		{"0", model.NoJudgeTimeout},
		{"10s", 30 * time.Second},
		{"2m", 2 * time.Minute},
		{"soon", time.Minute},
	} {
		if got := (model.VerifyConfig{JudgeTimeout: tc.value}).ResolvedJudgeTimeout(); got != tc.want {
			t.Errorf("judge_timeout %q reads %v, want %v", tc.value, got, tc.want)
		}
	}
	if bad := (model.VerifyConfig{JudgeTimeout: "soon"}).KnownValues(); bad.JudgeTimeout != "" {
		t.Error("an unreadable judge_timeout should be ignored")
	}
	twenty, zero := 20, 0
	if got := (model.VerifyConfig{}).ResolvedJudgeMinimum(); got != 50 {
		t.Errorf("minimum %d unset, want 50", got)
	}
	if got := (model.VerifyConfig{JudgeMinimum: &twenty}).ResolvedJudgeMinimum(); got != 20 {
		t.Errorf("minimum %d, want the set 20", got)
	}
	if got := (model.VerifyConfig{JudgeMinimum: &zero}).ResolvedJudgeMinimum(); got != 50 {
		t.Errorf("minimum %d for 0, want the default: a minimum counts at least one", got)
	}
	policy := (model.VerifyConfig{JudgeTimeout: "45s", JudgeMinimum: &twenty}).GroundingPolicy()
	if policy.JudgeTimeout != 45*time.Second || policy.JudgeMinimum != 20 {
		t.Errorf("policy %+v should carry the resolved judge limits", policy)
	}
}

// The stage a judge can reach counts only the unscored pairs it can read;
// without a filter it counts every one.
func TestReachableStage_CountsOnlyThePairsTheJudgeReads(t *testing.T) {
	var batch model.RelevanceBatch
	for i := range 80 {
		pair := model.NewRelevancePair(fmt.Sprintf("20250101-%06d-s-tac-pos", i), "20260201-000000-d-tac-sub")
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}})
		if i < 40 {
			batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: "j", Score: 200}})
		}
	}
	ledger := model.NewRelevanceLedger(batch)
	held, reachable, unscored := ledger.ReachableStage("j", nil)
	if held != 0.90 || reachable != 0.95 || unscored != 40 {
		t.Fatalf("held %.2f, reachable %.2f, %d unscored: every unscored pair should count without a filter", held, reachable, unscored)
	}
	blind := func(pair model.RelevancePair) bool { return pair.A < "20250101-000060-s-tac-pos" }
	held, reachable, unscored = ledger.ReachableStage("j", blind)
	if held != 0.90 || reachable != 0.90 || unscored != 20 {
		t.Fatalf("held %.2f, reachable %.2f, %d unscored: a pair the judge cannot read should count toward no stage", held, reachable, unscored)
	}
}
