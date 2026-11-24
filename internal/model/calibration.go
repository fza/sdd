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

// AuditLabels returns judge's labels of audit samples, latest first: by
// the later entry of each pair, which is the capture that judged it.
func AuditLabels(labels []RelevanceLabel) []RelevanceLabel {
	var audit []RelevanceLabel
	for _, label := range labels {
		if label.Audit && (label.Kind == LabelPositive || label.Kind == LabelNegative || label.Kind == LabelCounted) {
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
	misses, counted, _ = driftWindow(labels)
	return misses, counted
}

// driftWindow is DriftWindowMisses with the weighted miss share.
func driftWindow(labels []RelevanceLabel) (misses, counted int, share float64) {
	audit := AuditLabels(labels)
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
	_, counted, share := driftWindow(since)
	if counted < DriftMinimum || share <= DriftMissShare {
		return DriftStep{}, false
	}
	thresholds := Calibrate(labels, steps, judge)
	if !thresholds.Valid {
		return DriftStep{}, false
	}
	return DriftStep{Judge: judge, At: now.UTC(), SetAside: ScoreByte(thresholds.SetAside) / 2}, true
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
	// RecallBound the Wilson lower bound of recall at it.
	Misses      int
	RecallBound float64
	// AuditMisses of AuditCounted are the referenced audit samples in the
	// drift window.
	AuditMisses, AuditCounted int
	Steps                     []DriftStep
	// PassedInFull counts the labelled pairs scoring at or above the
	// relevant threshold, Referenced how many of them were referenced, and
	// SetAside how many were set aside with each code, the dominant first.
	PassedInFull int
	Referenced   int
	SetAside     []CodePrecision
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
	report := CalibrationReport{Judge: judge, Thresholds: Calibrate(labels, steps, judge), Unscored: len(l.UnscoredReadable(judge, readable)), Live: len(live), Replay: replayed, ReplayExcluded: excludeReplay}
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
	report.AuditMisses, report.AuditCounted = DriftWindowMisses(labels)
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
