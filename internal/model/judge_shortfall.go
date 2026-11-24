package model

import "time"

// JudgeShortfallCause is why a graph check's ranking judge scored fewer
// candidates than its minimum.
type JudgeShortfallCause string

const (
	// ShortfallFailed: the judge is unreachable or its calls fail.
	ShortfallFailed JudgeShortfallCause = "failed"
	// ShortfallCalibrating: background calibration is scoring with the judge.
	ShortfallCalibrating JudgeShortfallCause = "calibrating"
	// ShortfallUncalibrated: the judge holds no recall stage, or a lower one
	// than its unscored labelled pairs would give.
	ShortfallUncalibrated JudgeShortfallCause = "uncalibrated"
	// ShortfallSlow: the judge answers but is too slow for the list.
	ShortfallSlow JudgeShortfallCause = "slow"
)

// JudgeState is what tells the causes of a shortfall apart: the failures of
// the check's calls other than running out of time, the background
// calibration in progress, and the recall stage the judge holds against the
// one its unscored labelled pairs would give.
type JudgeState struct {
	Failures    []string
	Calibrating bool
	// CalibrationScored of CalibrationOf pairs the background run scored.
	CalibrationScored, CalibrationOf int
	Stage, Reachable                 float64
	// Unscored counts the judge's unscored labelled pairs.
	Unscored int
}

// JudgeShortfall is a graph check whose judge scored Judged of the Listed
// candidates, fewer than Minimum, with its cause. Waited is how long the
// check waited for the judge; Unattended marks a session without a person,
// where the agent judges from the lines.
type JudgeShortfall struct {
	Judged     int                 `json:"judged"`
	Listed     int                 `json:"listed"`
	Minimum    int                 `json:"minimum"`
	Cause      JudgeShortfallCause `json:"cause"`
	Failures   []string            `json:"failures,omitempty"`
	Waited     time.Duration       `json:"waited,omitempty"`
	Unattended bool                `json:"unattended,omitempty"`
	// The calibration in progress, for ShortfallCalibrating.
	CalibrationScored int `json:"calibration_scored,omitempty"`
	CalibrationOf     int `json:"calibration_of,omitempty"`
	// The stages and unscored pairs, for ShortfallUncalibrated.
	Stage     float64 `json:"stage,omitempty"`
	Reachable float64 `json:"reachable,omitempty"`
	Unscored  int     `json:"unscored,omitempty"`
}

// JudgeTarget is the count of candidates a check waits for: the minimum,
// or every candidate where the list is shorter.
func JudgeTarget(listed, minimum int) int {
	return min(listed, minimum)
}

// NewJudgeShortfall returns the shortfall of a check whose judge scored
// judged of listed candidates against minimum, and false when it reached
// JudgeTarget. The cause is the first that holds of: a call failed other
// than by running out of time, background calibration runs, the judge lacks
// a recall stage its labelled pairs would give, else it is too slow.
func NewJudgeShortfall(judged, listed, minimum int, waited time.Duration, state JudgeState) (JudgeShortfall, bool) {
	if judged >= JudgeTarget(listed, minimum) {
		return JudgeShortfall{}, false
	}
	shortfall := JudgeShortfall{Judged: judged, Listed: listed, Minimum: minimum, Waited: waited}
	switch {
	case len(state.Failures) > 0:
		shortfall.Cause, shortfall.Failures = ShortfallFailed, state.Failures
	case state.Calibrating:
		shortfall.Cause = ShortfallCalibrating
		shortfall.CalibrationScored, shortfall.CalibrationOf = state.CalibrationScored, state.CalibrationOf
	case state.Stage == 0 || state.Reachable > state.Stage:
		shortfall.Cause = ShortfallUncalibrated
		shortfall.Stage, shortfall.Reachable, shortfall.Unscored = state.Stage, state.Reachable, state.Unscored
	default:
		shortfall.Cause = ShortfallSlow
	}
	return shortfall, true
}

// Missing is how many more candidates the judge had to score.
func (s JudgeShortfall) Missing() int {
	return JudgeTarget(s.Listed, s.Minimum) - s.Judged
}

// JudgeKeepsJudging is how many missing candidates a check still waits past
// its time limit for, where a person is there to choose.
const JudgeKeepsJudging = 10

// ReachableStage returns the recall stage judge holds, the one it would
// hold once its unscored labelled pairs are scored and miss nothing, and
// how many pairs are unscored, those readable keeps alone
// (UnscoredReadable).
func (l *RelevanceLedger) ReachableStage(judge string, readable func(RelevancePair) bool) (held, reachable float64, unscored int) {
	labels := l.Labels(judge)
	held = Calibrate(labels, l.Steps(), judge).Stage
	positives := 0.0
	for _, label := range labels {
		if label.Kind == LabelPositive {
			positives += label.Weight
		}
	}
	pairs := l.UnscoredReadable(judge, readable)
	for _, pair := range pairs {
		verdict := l.readers[pair]
		if LabelKind(verdict) == LabelPositive {
			positives += LabelWeight(verdict)
		}
	}
	return held, max(held, StageHeld(positives)), len(pairs)
}
