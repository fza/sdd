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

// Audit and drift constants (20261109-004506-d-cpt-udz, D36).
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
	audit := AuditLabels(labels)
	if len(audit) > DriftWindow {
		audit = audit[:DriftWindow]
	}
	for _, label := range audit {
		if label.Kind == LabelPositive {
			misses++
		}
	}
	return misses, len(audit)
}

// DriftStepDue returns the step the drift rule takes now (D36): when the
// latest audit labels, at least DriftMinimum, hold more than DriftMissShare
// referenced samples and no step of the judge came within DriftInterval,
// the set-aside threshold halves. It never rises.
func DriftStepDue(labels []RelevanceLabel, steps []DriftStep, judge string, now time.Time) (DriftStep, bool) {
	misses, counted := DriftWindowMisses(labels)
	if counted < DriftMinimum || float64(misses)/float64(counted) <= DriftMissShare {
		return DriftStep{}, false
	}
	for _, step := range steps {
		if step.Judge == judge && now.Sub(step.At) < DriftInterval {
			return DriftStep{}, false
		}
	}
	thresholds := Calibrate(labels, steps, judge)
	if !thresholds.Valid {
		return DriftStep{}, false
	}
	return DriftStep{Judge: judge, At: now.UTC(), SetAside: ScoreByte(thresholds.SetAside) / 2}, true
}

// CodePrecision is how the pairs a judge would serve in full were answered,
// for one reason code (D11).
type CodePrecision struct {
	Code  string
	Count int
}

// CalibrationReport is what `sdd grounding status` shows of one judge.
type CalibrationReport struct {
	Judge      string
	Thresholds RelevanceThresholds
	// Labels by kind; Unscored counts the labelled pairs the judge has no
	// score for, re-scored after a judge change (D36).
	Positive, Negative, Counted, Excluded int
	Unscored                              int
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

// DominantCode is the code most pairs passed in full were set aside with:
// the direction a prompt revision takes (D11). Empty when none was.
func (r CalibrationReport) DominantCode() string {
	if len(r.SetAside) == 0 {
		return ""
	}
	return r.SetAside[0].Code
}

// Calibration reports judge's calibration over the ledger.
func (l *RelevanceLedger) Calibration(judge string) CalibrationReport {
	labels := l.Labels(judge)
	steps := l.Steps()
	report := CalibrationReport{Judge: judge, Thresholds: Calibrate(labels, steps, judge), Unscored: len(l.Unscored(judge))}
	for _, step := range steps {
		if step.Judge == judge {
			report.Steps = append(report.Steps, step)
		}
	}
	var positives []uint8
	byCode := map[string]int{}
	for _, label := range labels {
		switch label.Kind {
		case LabelPositive:
			report.Positive++
			positives = append(positives, label.Score)
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
	if report.Thresholds.Valid {
		for _, score := range positives {
			if ScoreValue(score) < report.Thresholds.SetAside {
				report.Misses++
			}
		}
	}
	report.RecallBound = WilsonLower(report.Positive-report.Misses, report.Positive)
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

// InsertBlind places audit samples among the lines to judge at positions
// drawn from seed, unscored, so nothing tells them from the band the
// prefilter left (D36).
func InsertBlind(toJudge, audit []GatherCandidate, seed string) []GatherCandidate {
	out := slices.Clone(toJudge)
	for _, candidate := range audit {
		candidate.Score = nil
		sum := sha256.Sum256([]byte(seed + "\naudit\n" + candidate.Entry.ID))
		position := int(binary.BigEndian.Uint64(sum[:8]) % uint64(len(out)+1))
		out = slices.Insert(out, position, candidate)
	}
	return out
}
