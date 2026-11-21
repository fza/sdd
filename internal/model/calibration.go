package model

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"slices"
	"strings"
	"time"
)

// Audit and drift constants.
const (
	// AuditShare is the share of the prefilter's set-asides shown blind as
	// ordinary lines to judge.
	AuditShare = 0.03
	// DriftWindow is how many of the latest audit labels the drift rule
	// reads; DriftMinimum how many it needs before it acts.
	DriftWindow  = 200
	DriftMinimum = 50
	// DriftMissShare is the referenced share of audit samples above which
	// the set-aside threshold steps down.
	DriftMissShare = 0.02
	// DriftInterval is the least time between two steps of one judge.
	DriftInterval = 24 * time.Hour
)

// AuditSample picks the set-aside entries shown blind: ceil(AuditShare of
// them), chosen by a hash of seed and ID so every gather of one reading
// picks the same ones. The rest stay set aside; both come back sorted.
func AuditSample(setAside []string, seed string) (audit, kept []string) {
	if len(setAside) == 0 {
		return nil, nil
	}
	count := int(math.Ceil(float64(len(setAside)) * AuditShare))
	ranked := slices.Clone(setAside)
	rank := func(id string) uint64 {
		sum := sha256.Sum256([]byte(seed + "\n" + id))
		return binary.BigEndian.Uint64(sum[:8])
	}
	slices.SortFunc(ranked, func(a, b string) int { return cmp.Compare(rank(a), rank(b)) })
	audit = slices.Clone(ranked[:count])
	kept = slices.Clone(ranked[count:])
	slices.Sort(audit)
	slices.Sort(kept)
	return audit, kept
}

// AuditLabels returns judge's labels of audit samples of the threshold's
// set-asides, latest first: by the later entry of each pair, which is the
// capture that judged it. A sample of a far floor set-aside is none of
// them (JudgeFloorHealth).
func AuditLabels(labels []RelevanceLabel) []RelevanceLabel {
	return auditLabels(labels, false)
}

// auditLabels returns the labels of audit samples the far floor set aside,
// or those it did not, latest first.
func auditLabels(labels []RelevanceLabel, floor bool) []RelevanceLabel {
	var audit []RelevanceLabel
	for _, label := range labels {
		if label.Audit && label.Floor == floor && (label.Kind == LabelPositive || label.Kind == LabelNegative || label.Kind == LabelCounted) {
			audit = append(audit, label)
		}
	}
	slices.SortStableFunc(audit, func(a, b RelevanceLabel) int {
		if c := strings.Compare(b.Pair.B, a.Pair.B); c != 0 {
			return c
		}
		return strings.Compare(b.Pair.A, a.Pair.A)
	})
	return audit
}

// DriftWindowMisses counts the referenced audit samples among the latest
// DriftWindow audit labels, and the labels counted.
func DriftWindowMisses(labels []RelevanceLabel) (misses, counted int) {
	misses, counted, _ = driftWindow(AuditLabels(labels))
	return misses, counted
}

// FloorHealth is how the far floor fared in a judge's audit: Misses of the
// latest Counted audit samples of its set-asides were referenced. Off turns
// the floor off for the judge.
type FloorHealth struct {
	Misses, Counted int
	Off             bool
}

// JudgeFloorHealth holds the far floor to the drift rule's bar: once the
// latest DriftWindow audit labels of its set-asides, at least DriftMinimum,
// hold a weighted share of referenced samples above DriftMissShare, the
// floor is off. An off floor sets nothing aside, so no later label turns it
// on again.
func JudgeFloorHealth(labels []RelevanceLabel) FloorHealth {
	misses, counted, share := driftWindow(auditLabels(labels, true))
	return FloorHealth{Misses: misses, Counted: counted, Off: counted >= DriftMinimum && share > DriftMissShare}
}

// driftWindow counts the referenced samples among the latest DriftWindow of
// audit, which runs latest first, the labels counted and the weighted miss
// share.
func driftWindow(audit []RelevanceLabel) (misses, counted int, share float64) {
	if len(audit) > DriftWindow {
		audit = audit[:DriftWindow]
	}
	missed, weight := 0.0, 0.0
	for _, label := range audit {
		weight += label.Weight
		if label.Kind == LabelPositive {
			misses++
			missed += label.Weight
		}
	}
	if weight > 0 {
		share = missed / weight
	}
	return misses, len(audit), share
}

// DriftStepDue returns the step the drift rule takes now: when the latest
// audit labels, at least DriftMinimum, hold a weighted share of referenced
// samples above DriftMissShare and no step of the judge came within
// DriftInterval, the set-aside threshold halves. It never rises. Only labels
// of captures after the judge's last step count, so a window the last step
// already answered never halves the threshold again.
func DriftStepDue(labels []RelevanceLabel, steps []DriftStep, judge string, now time.Time) (DriftStep, bool) {
	var last time.Time
	for _, step := range steps {
		if step.Judge != judge {
			continue
		}
		if now.Sub(step.At) < DriftInterval {
			return DriftStep{}, false
		}
		if step.At.After(last) {
			last = step.At
		}
	}
	since := labels
	if !last.IsZero() {
		// Entry IDs are stamped in the zone of the clock now comes from.
		mark := last.In(now.Location()).Format("20060102-150405")
		since = slices.DeleteFunc(slices.Clone(labels), func(label RelevanceLabel) bool { return label.Pair.B <= mark })
	}
	_, counted, share := driftWindow(AuditLabels(since))
	if counted < DriftMinimum || share <= DriftMissShare {
		return DriftStep{}, false
	}
	thresholds := Calibrate(labels, steps, judge)
	if !thresholds.Valid {
		return DriftStep{}, false
	}
	return DriftStep{Judge: judge, At: now.UTC(), SetAside: ScoreByte(thresholds.SetAside) / 2}, true
}

// CalibrationRun is a background calibration run a status names
// (application.CalibrationRun).
type CalibrationRun struct {
	Judge   string
	Started time.Time
	Stage   float64
	Scored  int
	Waiting bool
}

// CodePrecision is how the pairs a judge would serve in full were answered,
// for one reason code.
type CodePrecision struct {
	Code  string
	Count int
}

// CalibrationReport is what `sdd grounding status` shows of one judge.
type CalibrationReport struct {
	Judge      string
	Thresholds RelevanceThresholds
	// Labels by kind; Unscored counts the labelled pairs the judge has no
	// score for, re-scored after a judge change.
	Positive, Negative, Counted, Excluded int
	Unscored                              int
	// Live and Replay count the labels by source; ReplayExcluded reports
	// that the rest of the report reads the live labels alone.
	Live, Replay int
	// OpenedHidden counts the hidden entries the deep verifier opened with
	// no finding resting on them; it moves no threshold.
	OpenedHidden   int
	ReplayExcluded bool
	// Implicit counts the implicit dismissals the ledger holds, Flipped
	// those a later entry flipped to bearing.
	Implicit, Flipped int
	// Misses are the positives scoring below the set-aside threshold;
	// RecallBound the Wilson lower bound of recall at it; PositiveWeight the
	// weight of the positive labels, which decides the stage (StageHeld).
	Misses         int
	RecallBound    float64
	PositiveWeight float64
	// AuditMisses of AuditCounted are the referenced audit samples in the
	// drift window.
	AuditMisses, AuditCounted int
	Floor                     FloorHealth
	Steps                     []DriftStep
	// PassedInFull counts the labelled pairs scoring at or above the
	// relevant threshold, Referenced how many of them were referenced, and
	// SetAside how many were set aside with each code, the dominant first.
	PassedInFull int
	Referenced   int
	SetAside     []CodePrecision
	Ranking      RankingHealth
}

// The ranking health rule: a judge's ranking recall is the weighted share of
// its positive labels it scores at or above RankingRecallLine. Measured from
// RankingRecallMinimum positives on, a recall below RankingRecallBar makes
// the judge weak, and the deep reading it ranks widens its tiers.
const (
	RankingRecallLine    = 0.5
	RankingRecallBar     = 0.9
	RankingRecallMinimum = 20
)

// RankingHealth is a judge's ranking recall over its positive labels.
type RankingHealth struct {
	Positives int
	Recall    float64
	Measured  bool
}

// Weak reports a measured recall below RankingRecallBar.
func (h RankingHealth) Weak() bool { return h.Measured && h.Recall < RankingRecallBar }

// JudgeRankingHealth applies the ranking health rule to a judge's labels.
func JudgeRankingHealth(labels []RelevanceLabel) RankingHealth {
	var health RankingHealth
	total, above := 0.0, 0.0
	for _, label := range labels {
		if label.Kind != LabelPositive {
			continue
		}
		health.Positives++
		total += label.Weight
		if ScoreValue(label.Score) >= RankingRecallLine {
			above += label.Weight
		}
	}
	if total > 0 {
		health.Recall = above / total
	}
	health.Measured = health.Positives >= RankingRecallMinimum
	return health
}

// NextStage is the recall stage above the one the report's positive labels
// hold, and the positive weight it still needs; 0 for both at the top.
func (r CalibrationReport) NextStage() (stage, needed float64) {
	stage = NextStage(StageHeld(r.PositiveWeight))
	if stage == 0 {
		return 0, 0
	}
	return stage, StagePositives(stage) - r.PositiveWeight
}

// Precision is the referenced share of the pairs passed in full.
func (r CalibrationReport) Precision() float64 {
	if r.PassedInFull == 0 {
		return 0
	}
	return float64(r.Referenced) / float64(r.PassedInFull)
}

// FlipRate is the share of implicit dismissals flipped to bearing.
func (r CalibrationReport) FlipRate() float64 {
	if r.Implicit == 0 {
		return 0
	}
	return float64(r.Flipped) / float64(r.Implicit)
}

// DominantCode is the code most pairs passed in full were set aside with: the
// direction a prompt revision takes. Empty when none was.
func (r CalibrationReport) DominantCode() string {
	if len(r.SetAside) == 0 {
		return ""
	}
	return r.SetAside[0].Code
}

// Calibration reports judge's calibration over the ledger, over its live
// labels alone when excludeReplay is set.
func (l *RelevanceLedger) Calibration(judge string, excludeReplay bool) CalibrationReport {
	labels := l.Labels(judge)
	steps := l.Steps()
	live := LiveLabels(labels)
	replayed := len(labels) - len(live)
	if excludeReplay {
		labels = live
	}
	report := CalibrationReport{Judge: judge, Thresholds: Calibrate(labels, steps, judge), Unscored: len(l.Unscored(judge)), Live: len(live), Replay: replayed, ReplayExcluded: excludeReplay, Ranking: JudgeRankingHealth(labels)}
	for _, verdict := range l.readers {
		if verdict.OpenedHidden && verdict.Code != ReaderReferenced {
			report.OpenedHidden++
		}
		if verdict.Code == ReaderImplicit {
			report.Implicit++
			if verdict.Flipped {
				report.Flipped++
			}
		}
	}
	for _, step := range steps {
		if step.Judge == judge {
			report.Steps = append(report.Steps, step)
		}
	}
	var positives []RelevanceLabel
	byCode := map[string]int{}
	for _, label := range labels {
		switch label.Kind {
		case LabelPositive:
			report.Positive++
			positives = append(positives, label)
		case LabelNegative:
			report.Negative++
		case LabelCounted:
			report.Counted++
		default:
			report.Excluded++
		}
		if !report.Thresholds.Valid || ScoreValue(label.Score) < report.Thresholds.Relevant {
			continue
		}
		report.PassedInFull++
		if label.Kind == LabelPositive {
			report.Referenced++
		} else if label.Reason != "" {
			byCode[label.Reason]++
		}
	}
	total, missed := 0.0, 0.0
	for _, positive := range positives {
		total += positive.Weight
		if report.Thresholds.Valid && ScoreValue(positive.Score) < report.Thresholds.SetAside {
			report.Misses++
			missed += positive.Weight
		}
	}
	report.RecallBound = WilsonLowerWeighted(total-missed, total)
	report.PositiveWeight = total
	report.AuditMisses, report.AuditCounted = DriftWindowMisses(labels)
	report.Floor = JudgeFloorHealth(labels)
	for code, count := range byCode {
		report.SetAside = append(report.SetAside, CodePrecision{Code: code, Count: count})
	}
	slices.SortFunc(report.SetAside, func(a, b CodePrecision) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Code, b.Code)
	})
	return report
}

// InsertBlind places audit samples among the lines to judge at positions drawn
// from seed, unscored, so nothing tells them from the band the prefilter left.
func InsertBlind(toJudge, audit []GatherCandidate, seed string) []GatherCandidate {
	out := slices.Clone(toJudge)
	for _, candidate := range audit {
		candidate.Score = nil
		out = slices.Insert(out, blindPosition(seed, candidate.Entry.ID, len(out)), candidate)
	}
	return out
}

// blindPosition draws where an audit sample goes among n lines from seed.
func blindPosition(seed, id string, n int) int {
	sum := sha256.Sum256([]byte(seed + "\naudit\n" + id))
	return int(binary.BigEndian.Uint64(sum[:8]) % uint64(n+1))
}

// LiveLabels returns the labels of live captures, dropping those seeded
// from replayed ones.
func LiveLabels(labels []RelevanceLabel) []RelevanceLabel {
	return slices.DeleteFunc(slices.Clone(labels), func(label RelevanceLabel) bool { return label.Source != SourceLive })
}
