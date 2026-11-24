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

// AuditShare is the share of the entries the deep verifier's reading hides
// that it ranks blind among the ones it shows.
const AuditShare = 0.03

// AuditSample picks the set-aside entries shown blind: ceil(AuditShare of
// them), chosen by a hash of seed and ID so every gather of one reading
// picks the same ones. The rest stay hidden; both come back sorted.
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

// CalibrationRun is a background calibration run a status names
// (application.CalibrationRun).
type CalibrationRun struct {
	Judge     string
	Started   time.Time
	Stage     float64
	Scored    int
	Waiting   bool
	Replaying bool
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
	Steps          []DriftStep
	// PassedInFull counts the live labelled pairs scoring at or above the
	// relevant threshold, which rests on them alone, Referenced how many of
	// them were referenced, and SetAside how many were set aside with each
	// code, the dominant first.
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
// labels alone when excludeReplay is set. Unscored counts the pairs
// readable keeps (UnscoredReadable).
func (l *RelevanceLedger) Calibration(judge string, excludeReplay bool, readable func(RelevancePair) bool) CalibrationReport {
	labels := l.Labels(judge)
	steps := l.Steps()
	live := LiveLabels(labels)
	replayed := len(labels) - len(live)
	if excludeReplay {
		labels = live
	}
	report := CalibrationReport{Judge: judge, Thresholds: Calibrate(labels, steps, judge), Unscored: len(l.UnscoredReadable(judge, readable)), Live: len(live), Replay: replayed, ReplayExcluded: excludeReplay, Ranking: JudgeRankingHealth(labels)}
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
		if label.Source != SourceLive || !report.Thresholds.Valid || ScoreValue(label.Score) < report.Thresholds.Relevant {
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
