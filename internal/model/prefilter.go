package model

import (
	"cmp"
	"math"
	"slices"
	"strings"
)

// Prefilter constants (20261109-004506-d-cpt-udz, 5.13).
const (
	// RecallFloor is the Wilson lower bound (95%) of recall a judge reaches
	// before it sets any entry aside (D33).
	RecallFloor = 0.98
	// RelevantPrecision is the Wilson lower bound (95%) of the referenced
	// share above which a scored candidate is served in full.
	RelevantPrecision = 0.5
	wilsonZ           = 1.959963984540054
)

// Prefilter statuses a gather serve names.
const (
	PrefilterOff     = "off"
	PrefilterOK      = "ok"
	PrefilterPartial = "partial"
	PrefilterFailed  = "failed"
)

// PrefilterPayload is what one gather's prefilter adds to a capture's
// reading: the judge, the direction its scores answer from the draft, and a
// score byte per candidate.
type PrefilterPayload struct {
	Judge   string           `json:"judge"`
	Forward bool             `json:"forward,omitempty"`
	Scores  map[string]uint8 `json:"scores,omitempty"`
	// Audit names the set-aside candidates shown blind as lines to judge
	// (D36).
	Audit []string `json:"audit,omitempty"`
}

// readingScores is the reading's fold of prefilter scores per judge.
type readingScores struct {
	forward bool
	scores  map[string]uint8
}

// Scores returns the scores judge gave inside the reading, and whether they
// answer only from the draft to each candidate.
func (r *Reading) Scores(judge string) (map[string]uint8, bool) {
	if r == nil || r.scores[judge] == nil {
		return nil, false
	}
	held := r.scores[judge]
	return held.scores, held.forward
}

func (r *Reading) foldPrefilter(p *PrefilterPayload) {
	if p == nil || p.Judge == "" {
		return
	}
	if r.scores == nil {
		r.scores = map[string]*readingScores{}
	}
	held := r.scores[p.Judge]
	if held == nil {
		held = &readingScores{forward: p.Forward, scores: map[string]uint8{}}
		r.scores[p.Judge] = held
	}
	if r.audit == nil {
		r.audit = map[string]bool{}
	}
	for _, id := range p.Audit {
		r.audit[id] = true
	}
	for id, score := range p.Scores {
		if current, ok := held.scores[id]; !ok || score > current {
			held.scores[id] = score
		}
	}
}

// WilsonLower is the Wilson score interval's lower bound (95%) of the share
// successes/n.
func WilsonLower(successes, n int) float64 {
	if n <= 0 {
		return 0
	}
	p := float64(successes) / float64(n)
	z2 := wilsonZ * wilsonZ
	nf := float64(n)
	center := p + z2/(2*nf)
	margin := wilsonZ * math.Sqrt(p*(1-p)/nf+z2/(4*nf*nf))
	return (center - margin) / (1 + z2/nf)
}

// Label kinds of D11.
const (
	LabelPositive = "positive"
	LabelNegative = "negative"
	LabelCounted  = "counted"
	LabelExcluded = "excluded"
)

// RelevanceLabel is one judged pair a judge scored: the reader's answer as a
// calibration label (D11) with the judge's score.
type RelevanceLabel struct {
	Pair   RelevancePair
	Score  uint8
	Kind   string
	Reason string
	Audit  bool
}

// LabelKind classifies a reader record (D11): a reference is positive; a
// set-aside coded topic-only, other-scope or term-clash is negative; other
// is counted only; covered, meta and unknown codes are excluded.
func LabelKind(verdict ReaderVerdict) string {
	switch verdict.Code {
	case ReaderReferenced:
		return LabelPositive
	case ReaderNone:
		return LabelExcluded
	}
	switch VerdictReasonCode(verdict.Reason) {
	case VerdictTopicOnly, VerdictOtherScope, VerdictTermClash:
		return LabelNegative
	case VerdictOther:
		return LabelCounted
	}
	return LabelExcluded
}

// Labels returns every pair with a reader record that judge scored, sorted
// by pair. The subject of a pair is its later entry, the one whose reading
// judged the other.
func (l *RelevanceLedger) Labels(judge string) []RelevanceLabel {
	var labels []RelevanceLabel
	for _, pair := range l.Pairs() {
		score, ok := l.Score(pair, pair.B, judge)
		if !ok {
			continue
		}
		verdict := l.readers[pair]
		labels = append(labels, RelevanceLabel{Pair: pair, Score: score, Kind: LabelKind(verdict), Reason: VerdictReasonCode(verdict.Reason), Audit: verdict.Audit})
	}
	return labels
}

// Unscored returns the labelled pairs judge has no score for: what a judge
// change re-scores before its bound is computed (D36).
func (l *RelevanceLedger) Unscored(judge string) []RelevancePair {
	var pairs []RelevancePair
	for _, pair := range l.Pairs() {
		kind := LabelKind(l.readers[pair])
		if kind != LabelPositive && kind != LabelNegative {
			continue
		}
		if _, ok := l.Score(pair, pair.B, judge); !ok {
			pairs = append(pairs, pair)
		}
	}
	return pairs
}

// Calibrate computes a judge's thresholds from its labels (D33): the
// set-aside threshold is the highest score that keeps the Wilson lower
// bound of recall over the positive labels at RecallFloor; the relevant
// threshold the lowest score at and above which the referenced share's
// lower bound reaches RelevantPrecision. Until the recall bound holds, the
// thresholds are invalid and decide nothing. Drift steps cap the set-aside
// threshold; it never rises above a step (D36).
func Calibrate(labels []RelevanceLabel, steps []DriftStep, judge string) RelevanceThresholds {
	var positives []uint8
	for _, label := range labels {
		if label.Kind == LabelPositive {
			positives = append(positives, label.Score)
		}
	}
	slices.Sort(positives)
	n := len(positives)
	setAside := -1
	for misses := 0; misses < n; misses++ {
		if WilsonLower(n-misses, n) < RecallFloor {
			break
		}
		if misses > 0 && positives[misses] == positives[misses-1] {
			continue
		}
		setAside = int(positives[misses])
	}
	if setAside < 0 {
		return RelevanceThresholds{}
	}
	for _, step := range steps {
		if step.Judge == judge && int(step.SetAside) < setAside {
			setAside = int(step.SetAside)
		}
	}
	return RelevanceThresholds{Valid: true, SetAside: ScoreValue(uint8(setAside)), Relevant: relevantThreshold(labels)}
}

func relevantThreshold(labels []RelevanceLabel) float64 {
	judged := slices.DeleteFunc(slices.Clone(labels), func(label RelevanceLabel) bool {
		return label.Kind != LabelPositive && label.Kind != LabelNegative
	})
	slices.SortFunc(judged, func(a, b RelevanceLabel) int { return cmp.Compare(b.Score, a.Score) })
	relevant := 2.0
	positive := 0
	for i, label := range judged {
		if label.Kind == LabelPositive {
			positive++
		}
		if i+1 < len(judged) && judged[i+1].Score == label.Score {
			continue
		}
		if WilsonLower(positive, i+1) >= RelevantPrecision {
			relevant = ScoreValue(label.Score)
		}
	}
	return relevant
}

// Prefiltered is where a scored reading places its candidates.
type Prefiltered struct {
	Relevant []GatherCandidate
	ToJudge  []GatherCandidate
	SetAside []string
}

// PlacePrefiltered places candidates by their scores: with valid thresholds
// a score at or above Relevant is served in full and one below SetAside set
// aside; every other candidate, scored or not, stays to judge, scored ones
// first by falling score. Without valid thresholds nothing is set aside or
// served: the prefilter only sorts (D33).
func PlacePrefiltered(candidates []GatherCandidate, scores map[string]uint8, thresholds RelevanceThresholds) Prefiltered {
	var placed Prefiltered
	for _, candidate := range candidates {
		score, ok := scores[candidate.Entry.ID]
		if ok {
			value := ScoreValue(score)
			candidate.Score = &value
		}
		switch {
		case ok && thresholds.Valid && ScoreValue(score) >= thresholds.Relevant:
			placed.Relevant = append(placed.Relevant, candidate)
		case ok && thresholds.Valid && ScoreValue(score) < thresholds.SetAside:
			placed.SetAside = append(placed.SetAside, candidate.Entry.ID)
		default:
			placed.ToJudge = append(placed.ToJudge, candidate)
		}
	}
	byScore := func(a, b GatherCandidate) int {
		switch {
		case a.Score != nil && b.Score == nil:
			return -1
		case a.Score == nil && b.Score != nil:
			return 1
		case a.Score != nil && b.Score != nil && *a.Score != *b.Score:
			return cmp.Compare(*b.Score, *a.Score)
		}
		return 0
	}
	slices.SortStableFunc(placed.ToJudge, byScore)
	slices.SortStableFunc(placed.Relevant, byScore)
	slices.Sort(placed.SetAside)
	return placed
}

// RelevanceText is the whole text the prefilter reads of an entry (D30):
// kind, topics, title and body, never cut.
func RelevanceText(kind string, topics []string, title, body string) string {
	var b strings.Builder
	b.WriteString("kind: " + kind + "\n")
	if len(topics) > 0 {
		b.WriteString("topics: " + strings.Join(topics, ", ") + "\n")
	}
	if title != "" {
		b.WriteString("title: " + title + "\n")
	}
	b.WriteString("\n" + strings.TrimSpace(body))
	return b.String()
}

// EntryRelevanceText is RelevanceText of an entry with its effective topics.
func EntryRelevanceText(g *Graph, e *Entry) string {
	var topics []string
	for _, topic := range g.EffectiveTopics(e) {
		topics = append(topics, topic.String())
	}
	return RelevanceText(string(e.Kind), topics, e.Title, e.Content)
}
