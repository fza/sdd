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
	// Below names the candidates of Scores the far floor put below it
	// (FarFloor.Below), the verdict of the call that scored each.
	Below []string `json:"below,omitempty"`
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
// against the latest draft it scored, and the candidates among them below
// the far floor.
type readingScores struct {
	forward bool
	subject string
	scores  map[string]uint8
	below   map[string]bool
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

// BelowFloor returns the candidates judge's scores inside the reading put
// below the far floor against the draft subject keys, sorted.
func (r *Reading) BelowFloor(judge, subject string) []string {
	if r == nil || r.scores[judge] == nil || r.scores[judge].subject != subject || len(r.scores[judge].below) == 0 {
		return nil
	}
	return sortedKeys(r.scores[judge].below)
}

// audited returns verdict with the audit bit of a candidate the reading
// showed blind as an audit sample, and the floor bit of an audit sample the
// far floor had set aside.
func (r *Reading) audited(id string, verdict ReaderVerdict) ReaderVerdict {
	if r == nil {
		return verdict
	}
	verdict.Audit = verdict.Audit || r.audit[id]
	if !verdict.Audit {
		return verdict
	}
	for _, held := range r.scores {
		verdict.Floor = verdict.Floor || held.below[id]
	}
	return verdict
}

// foldPrefilter joins one read's scores and far floor verdicts. A score
// folds to the highest; a verdict stands as first given, so no later
// judgment of a candidate moves it across the floor.
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
		held = &readingScores{forward: p.Forward, subject: p.Subject, scores: map[string]uint8{}, below: map[string]bool{}}
		r.scores[judge] = held
	}
	if r.audit == nil {
		r.audit = map[string]bool{}
	}
	for _, id := range p.Audit {
		r.audit[id] = true
	}
	for _, id := range p.Below {
		if _, scored := held.scores[id]; !scored {
			held.below[id] = true
		}
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
// calibration label with the judge's score and the label's weight. Floor
// marks an audit sample of a far floor set-aside.
type RelevanceLabel struct {
	Pair   RelevancePair
	Score  uint8
	Kind   string
	Reason string
	Audit  bool
	Floor  bool
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
		labels = append(labels, RelevanceLabel{Pair: pair, Score: score, Kind: LabelKind(verdict), Reason: VerdictReasonCode(verdict.Reason), Audit: verdict.Audit, Floor: verdict.Floor, Source: verdict.Source, Weight: LabelWeight(verdict)})
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
// judge's thresholds rest on, rising. Below the first a judge has no
// threshold, and only the far floor sets aside.
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
// positive labels hold (StageHeld). The set-aside threshold is the highest
// score that keeps the lower bound of weighted recall over the positive
// labels at that stage (setAsideCut), or the lower cut of the live labels
// once they hold a stage alone: a replayed reference is an easier positive
// than an entry a live reading had to find. The relevant threshold is the
// lowest score, never below the set-aside threshold, at and above which
// the lower bound of the weighted positive share of the live labels
// reaches RelevantPrecision: replayed labels are positives only, so
// counting them would hold that share at every score. Below the first
// stage the thresholds are invalid and decide nothing. Drift steps cap the
// set-aside threshold; it never rises above a step.
func Calibrate(labels []RelevanceLabel, steps []DriftStep, judge string) RelevanceThresholds {
	stage, setAside := setAsideCut(labels)
	if stage == 0 {
		return RelevanceThresholds{}
	}
	live := LiveLabels(labels)
	fromLive := false
	if liveStage, cut := setAsideCut(live); liveStage > 0 && cut < setAside {
		setAside, fromLive = cut, true
	}
	for _, step := range steps {
		if step.Judge == judge && step.SetAside < setAside {
			setAside = step.SetAside
		}
	}
	return RelevanceThresholds{Valid: true, Stage: stage, SetAside: ScoreValue(setAside), SetAsideLive: fromLive, Relevant: relevantThreshold(live, setAside)}
}

// setAsideCut is the highest recall stage the positive labels hold and the
// highest score that keeps the lower bound of their weighted recall at it;
// stage 0 holds no cut.
func setAsideCut(labels []RelevanceLabel) (stage float64, cut uint8) {
	var positives []RelevanceLabel
	total := 0.0
	for _, label := range labels {
		if label.Kind == LabelPositive {
			positives = append(positives, label)
			total += label.Weight
		}
	}
	stage = StageHeld(total)
	if stage == 0 {
		return 0, 0
	}
	slices.SortStableFunc(positives, func(a, b RelevanceLabel) int { return cmp.Compare(a.Score, b.Score) })
	missed := 0.0
	for i, positive := range positives {
		if WilsonLowerWeighted(total-missed, total) < stage {
			break
		}
		missed += positive.Weight
		if i > 0 && positive.Score == positives[i-1].Score {
			continue
		}
		cut = positive.Score
	}
	return stage, cut
}

// noRelevantThreshold lies above every score: nothing is served in full.
const noRelevantThreshold = 2.0

func relevantThreshold(labels []RelevanceLabel, floor uint8) float64 {
	judged := slices.DeleteFunc(slices.Clone(labels), func(label RelevanceLabel) bool {
		return label.Kind != LabelPositive && label.Kind != LabelNegative
	})
	slices.SortFunc(judged, func(a, b RelevanceLabel) int { return cmp.Compare(b.Score, a.Score) })
	relevant := noRelevantThreshold
	positive, weight := 0.0, 0.0
	for i, label := range judged {
		if label.Score < floor {
			break
		}
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

// The tiers of a capture's reading while its judge holds no recall stage.
// They match the deep reading's summary and title tiers, so drafter and
// verifier read the same cut of one ranking.
const (
	GatherJudgeRanked = DeepSummaryRanked
	GatherTitleListed = DeepTitleListed
)

// Prefiltered is where a scored reading places its candidates: Listed go by
// title below the lines to judge, Counted past the title ceiling. BelowFloor
// are set aside by the far floor, SetAside by the calibrated threshold.
type Prefiltered struct {
	Relevant   []GatherCandidate
	ToJudge    []GatherCandidate
	Listed     []GatherCandidate
	Counted    []string
	SetAside   []string
	BelowFloor []string
}

// PlacePrefiltered places candidates by their scores. A candidate in
// belowFloor is set aside, with and without valid thresholds. Of the
// others, with valid thresholds a score below SetAside is set aside and any
// other at or above Relevant served in full; every other candidate, scored
// or not, stays to judge, scored ones first by falling score. Without valid
// thresholds no score sets aside or serves, and the scored candidates past
// the first GatherJudgeRanked, doubled when widened for a weak judge, are
// listed by title up to GatherTitleListed and counted past it. An unscored
// candidate has no rank and stays to judge.
func PlacePrefiltered(candidates []GatherCandidate, scores map[string]uint8, belowFloor map[string]bool, thresholds RelevanceThresholds, widened bool) Prefiltered {
	var placed Prefiltered
	for _, candidate := range candidates {
		score, ok := scores[candidate.Entry.ID]
		if ok {
			value := ScoreValue(score)
			candidate.Score = &value
		}
		switch {
		case belowFloor[candidate.Entry.ID]:
			placed.BelowFloor = append(placed.BelowFloor, candidate.Entry.ID)
		case ok && thresholds.Valid && ScoreValue(score) < thresholds.SetAside:
			placed.SetAside = append(placed.SetAside, candidate.Entry.ID)
		case ok && thresholds.Valid && ScoreValue(score) >= thresholds.Relevant:
			placed.Relevant = append(placed.Relevant, candidate)
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
	slices.Sort(placed.BelowFloor)
	if !thresholds.Valid {
		limit := GatherJudgeRanked
		if widened {
			limit *= 2
		}
		var kept []GatherCandidate
		ranked := 0
		for _, candidate := range placed.ToJudge {
			switch {
			case candidate.Score == nil || ranked < limit:
				kept = append(kept, candidate)
				if candidate.Score != nil {
					ranked++
				}
			case len(placed.Listed) < GatherTitleListed:
				placed.Listed = append(placed.Listed, candidate)
			default:
				placed.Counted = append(placed.Counted, candidate.Entry.ID)
			}
		}
		placed.ToJudge = kept
	}
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
