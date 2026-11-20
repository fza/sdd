package model

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"slices"
	"strings"
)

// Prefilter constants.
const (
	// RecallFloor is the top recall stage (RecallStages).
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
// reading: the judge, the direction its scores answer from the draft, the
// draft they answer (RelevanceSubjectHash), and a score byte per candidate.
type PrefilterPayload struct {
	Judge   string           `json:"judge"`
	Forward bool             `json:"forward,omitempty"`
	Subject string           `json:"subject,omitempty"`
	Scores  map[string]uint8 `json:"scores,omitempty"`
	// Audit names the set-aside candidates shown blind as lines to judge.
	Audit []string `json:"audit,omitempty"`
}

// RelevanceSubjectHash keys the scores a judge gave against one draft text
// (RelevanceText): a revised draft has its candidates scored again.
func RelevanceSubjectHash(subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return hex.EncodeToString(sum[:8])
}

// readingScores is the reading's fold of one judge's prefilter scores, all
// against the latest draft it scored.
type readingScores struct {
	forward bool
	subject string
	scores  map[string]uint8
}

// Scores returns the scores judge gave inside the reading against the draft
// subject keys, and whether they answer only from the draft to each
// candidate. Scores against an earlier draft are none.
func (r *Reading) Scores(judge, subject string) (map[string]uint8, bool) {
	if r == nil || r.scores[judge] == nil || r.scores[judge].subject != subject {
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
	judge := CanonicalJudge(p.Judge)
	held := r.scores[judge]
	if held == nil || held.subject != p.Subject {
		held = &readingScores{forward: p.Forward, subject: p.Subject, scores: map[string]uint8{}}
		r.scores[judge] = held
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
	return WilsonLowerWeighted(float64(successes), float64(n))
}

// WilsonLowerWeighted is WilsonLower over weighted labels: n is the sum of
// the labels' weights and successes the sum over the successful ones, so
// with p = successes/n and z = 1.96 the bound is
//
//	(p + z²/2n - z·sqrt(p(1-p)/n + z²/4n²)) / (1 + z²/n)
//
// A label of weight w counts as w of a whole one: four implicit dismissals
// at 0.25 move the bound as one explicit answer does.
func WilsonLowerWeighted(successes, n float64) float64 {
	if n <= 0 {
		return 0
	}
	p := min(max(successes/n, 0), 1)
	z2 := wilsonZ * wilsonZ
	center := p + z2/(2*n)
	margin := wilsonZ * math.Sqrt(p*(1-p)/n+z2/(4*n*n))
	return (center - margin) / (1 + z2/n)
}

// Label kinds: how a reader record counts as a calibration label.
const (
	LabelPositive = "positive"
	LabelNegative = "negative"
	LabelCounted  = "counted"
	LabelExcluded = "excluded"
)

// Label weights by reader state: how much one label counts in calibration.
const (
	WeightReferenced = 1.0
	WeightOpened     = 0.5
	WeightDismissed  = 1.0
	WeightImplicit   = 0.25
)

// RelevanceLabel is one judged pair a judge scored: the reader's answer as a
// calibration label with the judge's score and the label's weight.
type RelevanceLabel struct {
	Pair   RelevancePair
	Score  uint8
	Kind   string
	Reason string
	Audit  bool
	Source ReaderSource
	Weight float64
}

// LabelKind classifies a reader record: a reference and a flipped implicit
// dismissal are positive; an implicit dismissal and a set-aside coded
// topic-only, other-scope or term-clash are negative; other is counted only;
// covered, meta, no code and unknown codes are excluded.
func LabelKind(verdict ReaderVerdict) string {
	switch verdict.Code {
	case ReaderReferenced:
		return LabelPositive
	case ReaderImplicit:
		if verdict.Flipped {
			return LabelPositive
		}
		return LabelNegative
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

// LabelWeight is the weight of a reader record's label by its state; a
// flip keeps the implicit weight.
func LabelWeight(verdict ReaderVerdict) float64 {
	switch verdict.Code {
	case ReaderReferenced:
		return WeightReferenced
	case ReaderOpened:
		return WeightOpened
	case ReaderDismissed:
		return WeightDismissed
	case ReaderImplicit:
		return WeightImplicit
	}
	return 0
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
		labels = append(labels, RelevanceLabel{Pair: pair, Score: score, Kind: LabelKind(verdict), Reason: VerdictReasonCode(verdict.Reason), Audit: verdict.Audit, Source: verdict.Source, Weight: LabelWeight(verdict)})
	}
	return labels
}

// Scorable returns the labelled pairs a score calibrates: those labelled
// positive or negative, sorted by pair.
func (l *RelevanceLedger) Scorable() []RelevancePair {
	var pairs []RelevancePair
	for _, pair := range l.Pairs() {
		if kind := LabelKind(l.readers[pair]); kind == LabelPositive || kind == LabelNegative {
			pairs = append(pairs, pair)
		}
	}
	return pairs
}

// Unscored returns the labelled pairs judge has no score for: what a judge
// change re-scores before its bound is computed.
func (l *RelevanceLedger) Unscored(judge string) []RelevancePair {
	var pairs []RelevancePair
	for _, pair := range l.Scorable() {
		if _, ok := l.Score(pair, pair.B, judge); !ok {
			pairs = append(pairs, pair)
		}
	}
	return pairs
}

// RecallStages are the Wilson lower bounds (95%) of weighted recall a
// judge's thresholds rest on, rising. Below the first a judge sets nothing
// aside.
func RecallStages() []float64 {
	return []float64{0.90, 0.95, RecallFloor}
}

// StagePositives is the weight of positive labels at which a judge that
// misses none reaches stage: the Wilson bound of n successes of n is
// n/(n+z²).
func StagePositives(stage float64) float64 {
	return stage / (1 - stage) * wilsonZ * wilsonZ
}

// StageHeld is the highest recall stage positive labels weighing positives
// hold with no miss, 0 below the first.
func StageHeld(positives float64) float64 {
	held := 0.0
	for _, stage := range RecallStages() {
		if WilsonLowerWeighted(positives, positives) >= stage {
			held = stage
		}
	}
	return held
}

// NextStage is the lowest recall stage above stage, 0 above the top.
func NextStage(stage float64) float64 {
	for _, next := range RecallStages() {
		if next > stage {
			return next
		}
	}
	return 0
}

// Calibrate computes a judge's thresholds from its labels, each counted by
// its weight (WilsonLowerWeighted), at the highest recall stage the
// positive labels hold (StageHeld): the set-aside threshold is the highest
// score that keeps the lower bound of weighted recall over the positive
// labels at that stage; the relevant threshold the lowest score at and
// above which the weighted positive share's lower bound reaches
// RelevantPrecision. Below the first stage the thresholds are invalid and
// decide nothing. Drift steps cap the set-aside threshold; it never rises
// above a step.
func Calibrate(labels []RelevanceLabel, steps []DriftStep, judge string) RelevanceThresholds {
	var positives []RelevanceLabel
	total := 0.0
	for _, label := range labels {
		if label.Kind == LabelPositive {
			positives = append(positives, label)
			total += label.Weight
		}
	}
	stage := StageHeld(total)
	if stage == 0 {
		return RelevanceThresholds{}
	}
	slices.SortStableFunc(positives, func(a, b RelevanceLabel) int { return cmp.Compare(a.Score, b.Score) })
	setAside := -1
	missed := 0.0
	for i, positive := range positives {
		if WilsonLowerWeighted(total-missed, total) < stage {
			break
		}
		missed += positive.Weight
		if i > 0 && positive.Score == positives[i-1].Score {
			continue
		}
		setAside = int(positive.Score)
	}
	if setAside < 0 {
		return RelevanceThresholds{}
	}
	for _, step := range steps {
		if step.Judge == judge && int(step.SetAside) < setAside {
			setAside = int(step.SetAside)
		}
	}
	return RelevanceThresholds{Valid: true, Stage: stage, SetAside: ScoreValue(uint8(setAside)), Relevant: relevantThreshold(labels)}
}

func relevantThreshold(labels []RelevanceLabel) float64 {
	judged := slices.DeleteFunc(slices.Clone(labels), func(label RelevanceLabel) bool {
		return label.Kind != LabelPositive && label.Kind != LabelNegative
	})
	slices.SortFunc(judged, func(a, b RelevanceLabel) int { return cmp.Compare(b.Score, a.Score) })
	relevant := 2.0
	positive, weight := 0.0, 0.0
	for i, label := range judged {
		weight += label.Weight
		if label.Kind == LabelPositive {
			positive += label.Weight
		}
		if i+1 < len(judged) && judged[i+1].Score == label.Score {
			continue
		}
		if WilsonLowerWeighted(positive, weight) >= RelevantPrecision {
			relevant = ScoreValue(label.Score)
		}
	}
	return relevant
}

// The tiers of a capture's reading: ranked candidates past the first
// GatherJudgeRanked are listed by title up to GatherTitleListed. The cut sits
// below the deep reading's summary tier: every line to judge costs the
// drafter a judgment.
const (
	GatherJudgeRanked = 30
	GatherTitleListed = DeepTitleListed
)

// Prefiltered is where a scored reading places its candidates: Listed go by
// title below the lines to judge, Counted past the title ceiling. Ranked
// counts the lines to judge that hold a rank.
type Prefiltered struct {
	Relevant []GatherCandidate
	ToJudge  []GatherCandidate
	Listed   []GatherCandidate
	Counted  []string
	SetAside []string
	Ranked   int
}

// PlacePrefiltered places candidates by their scores: with valid thresholds a
// score at or above Relevant is served in full and one below SetAside set
// aside; every other candidate stays to judge. Lines to judge run open
// entries before done ones, then scored before unscored by falling score,
// then unscored by falling similarity to the draft, those similarity did not
// reach last. A candidate holds a rank when it is scored and the thresholds
// are invalid, or unscored with similarity read (non-nil similarity). Ranked
// candidates past the first GatherJudgeRanked, doubled when widened for a
// weak judge, are listed by title up to GatherTitleListed and counted past
// it; the others stay to judge.
func PlacePrefiltered(candidates []GatherCandidate, scores map[string]uint8, similarity map[string]float64, thresholds RelevanceThresholds, widened bool) Prefiltered {
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
	byRank := func(a, b GatherCandidate) int {
		if c := cmp.Compare(rankLast(a.Entry.Kind == KindDone), rankLast(b.Entry.Kind == KindDone)); c != 0 {
			return c
		}
		if c := byScore(a, b); c != 0 || a.Score != nil {
			return c
		}
		similarA, nearA := similarity[a.Entry.ID]
		similarB, nearB := similarity[b.Entry.ID]
		if c := cmp.Compare(rankLast(!nearA), rankLast(!nearB)); c != 0 {
			return c
		}
		return cmp.Compare(similarB, similarA)
	}
	slices.SortStableFunc(placed.ToJudge, byRank)
	slices.SortStableFunc(placed.Relevant, byScore)
	slices.Sort(placed.SetAside)
	limit := GatherJudgeRanked
	if widened {
		limit *= 2
	}
	var kept []GatherCandidate
	for _, candidate := range placed.ToJudge {
		ranked := candidate.Score != nil && !thresholds.Valid || candidate.Score == nil && similarity != nil
		switch {
		case !ranked:
			kept = append(kept, candidate)
		case placed.Ranked < limit:
			kept = append(kept, candidate)
			placed.Ranked++
		case len(placed.Listed) < GatherTitleListed:
			placed.Listed = append(placed.Listed, candidate)
		default:
			placed.Counted = append(placed.Counted, candidate.Entry.ID)
		}
	}
	placed.ToJudge = kept
	return placed
}

// RelevanceText is the whole text the prefilter reads of an entry: kind,
// topics, title and body, never cut.
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

// EntryRankingLine is the one line a chat judge reads of an entry:
// [type/kind], then title and summary, the summary alone for an untitled
// entry, the first sentence of the body for one without a summary.
func EntryRankingLine(e *Entry) string {
	summary := strings.Join(strings.Fields(e.Summary), " ")
	if summary == "" {
		summary = FirstSentence(e.Content)
	}
	text := summary
	if e.Title != "" {
		text = e.Title + ": " + summary
	}
	return "[" + string(e.Type) + "/" + string(e.Kind) + "] " + text
}

// EntryRelevanceText is RelevanceText of an entry with its effective topics.
func EntryRelevanceText(g *Graph, e *Entry) string {
	var topics []string
	for _, topic := range g.EffectiveTopics(e) {
		topics = append(topics, topic.String())
	}
	return RelevanceText(string(e.Kind), topics, e.Title, e.Content)
}
