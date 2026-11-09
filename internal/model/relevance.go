package model

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// RelevancePair is the unordered pair of entries a relevance verdict judges
// (20261109-004506-d-cpt-udz, 5.7): relevance is symmetric, so A sorts
// before B.
type RelevancePair struct {
	A string
	B string
}

// NewRelevancePair returns the pair of x and y in canonical order.
func NewRelevancePair(x, y string) RelevancePair {
	if y < x {
		x, y = y, x
	}
	return RelevancePair{A: x, B: y}
}

// Other returns the member of the pair that is not id.
func (p RelevancePair) Other(id string) string {
	if p.A == id {
		return p.B
	}
	return p.A
}

// Has reports whether id is a member of the pair.
func (p RelevancePair) Has(id string) bool { return p.A == id || p.B == id }

// ReaderCode is how far a reader went with a candidate, ordered as a
// lattice: a later, deeper answer never downgrades.
type ReaderCode uint8

const (
	ReaderNone ReaderCode = iota
	// ReaderDismissed is a candidate set aside from its listing line.
	ReaderDismissed
	// ReaderOpened is a candidate read in full and then set aside.
	ReaderOpened
	// ReaderReferenced is a candidate the entry references.
	ReaderReferenced
)

// String names the code as JSON exports carry it.
func (c ReaderCode) String() string {
	switch c {
	case ReaderNone:
		return "none"
	case ReaderDismissed:
		return "dismissed"
	case ReaderOpened:
		return "opened"
	case ReaderReferenced:
		return "referenced"
	}
	return fmt.Sprintf("code-%d", uint8(c))
}

// ParseReaderCode is String's inverse.
func ParseReaderCode(s string) (ReaderCode, error) {
	for c := ReaderNone; c <= ReaderReferenced; c++ {
		if c.String() == s {
			return c, nil
		}
	}
	return 0, fmt.Errorf("unknown reader code %q (none, dismissed, opened, referenced)", s)
}

// Verdict reason bytes. Each code holds its own byte so codes added later
// keep the format; 5 is the slot of the removed superseded code (D10, D12)
// and stays free.
var verdictReasonBytes = map[string]uint8{
	VerdictTopicOnly:  1,
	VerdictOtherScope: 2,
	VerdictTermClash:  3,
	VerdictCovered:    4,
	VerdictMeta:       6,
	VerdictOther:      7,
}

// VerdictReasonByte returns the ledger byte of a verdict code, 0 for none.
func VerdictReasonByte(code string) (uint8, error) {
	if code == "" {
		return 0, nil
	}
	b, ok := verdictReasonBytes[code]
	if !ok {
		return 0, fmt.Errorf("unknown verdict code %q (%s)", code, strings.Join(GroundingVerdictCodes(), ", "))
	}
	return b, nil
}

// VerdictReasonCode names a ledger reason byte: the code, "" for none, or
// "reason-N" for a byte this sdd does not know, which a reader tolerates.
func VerdictReasonCode(b uint8) string {
	if b == 0 {
		return ""
	}
	for code, value := range verdictReasonBytes {
		if value == b {
			return code
		}
	}
	return fmt.Sprintf("reason-%d", b)
}

// ParseVerdictReason is VerdictReasonCode's inverse.
func ParseVerdictReason(s string) (uint8, error) {
	var unknown uint8
	if _, err := fmt.Sscanf(s, "reason-%d", &unknown); err == nil && unknown > 0 {
		return unknown, nil
	}
	return VerdictReasonByte(s)
}

// Relation bits: how two entries strain each other (5.9).
const (
	RelationContradicts uint8 = 1 << iota
	RelationDuplicates
	RelationOutdates
)

// ReaderVerdict is a reader's record of one pair: the lattice code, the
// reason byte of a set-aside, whether the candidate was an audit sample of
// the prefilter's set-asides, and the strain relation bits.
type ReaderVerdict struct {
	Code      ReaderCode
	Reason    uint8
	Audit     bool
	Relations uint8
}

// Join merges two records of one pair. The code is the larger, the reason
// that of the larger code (the larger byte on a tie), and the bits are
// ORed, so merges converge in any order and repeat.
func (v ReaderVerdict) Join(o ReaderVerdict) ReaderVerdict {
	out := ReaderVerdict{Audit: v.Audit || o.Audit, Relations: v.Relations | o.Relations}
	switch {
	case v.Code > o.Code:
		out.Code, out.Reason = v.Code, v.Reason
	case o.Code > v.Code:
		out.Code, out.Reason = o.Code, o.Reason
	default:
		out.Code, out.Reason = v.Code, max(v.Reason, o.Reason)
	}
	return out
}

// RelevanceDirection is the direction a score answers. A judge that reads
// both texts answers both; a one-way judge (a reranker) answers only from
// the subject to the candidate.
type RelevanceDirection uint8

const (
	DirectionBoth RelevanceDirection = iota
	// DirectionAToB answers from the pair's A to its B.
	DirectionAToB
	// DirectionBToA answers from the pair's B to its A.
	DirectionBToA
)

// String names the direction as JSON exports carry it.
func (d RelevanceDirection) String() string {
	switch d {
	case DirectionBoth:
		return "both"
	case DirectionAToB:
		return "a-to-b"
	case DirectionBToA:
		return "b-to-a"
	}
	return fmt.Sprintf("direction-%d", uint8(d))
}

// ParseRelevanceDirection is String's inverse.
func ParseRelevanceDirection(s string) (RelevanceDirection, error) {
	for d := DirectionBoth; d <= DirectionBToA; d++ {
		if d.String() == s {
			return d, nil
		}
	}
	return 0, fmt.Errorf("unknown direction %q (both, a-to-b, b-to-a)", s)
}

// DirectionFrom returns the direction of a one-way score from subject to the
// other member of pair.
func DirectionFrom(pair RelevancePair, subject string) RelevanceDirection {
	if pair.A == subject {
		return DirectionAToB
	}
	return DirectionBToA
}

// ScoreByte quantizes a score in [0, 1] to the ledger's byte.
func ScoreByte(score float64) uint8 {
	switch {
	case score <= 0:
		return 0
	case score >= 1:
		return 255
	}
	return uint8(score*255 + 0.5)
}

// ScoreValue is ScoreByte's inverse.
func ScoreValue(b uint8) float64 { return float64(b) / 255 }

// FilterScore is one judge's score of a pair in one direction.
type FilterScore struct {
	Judge     string
	Direction RelevanceDirection
	Score     uint8
}

// ReaderRecord and ScoreRecord are the ledger's records.
type ReaderRecord struct {
	Pair    RelevancePair
	Verdict ReaderVerdict
}

type ScoreRecord struct {
	Pair  RelevancePair
	Score FilterScore
}

// RelevanceBatch is what one write adds to the ledger.
type RelevanceBatch struct {
	Readers []ReaderRecord
	Scores  []ScoreRecord
	Steps   []DriftStep
}

// Empty reports whether the batch adds nothing.
func (b RelevanceBatch) Empty() bool {
	return len(b.Readers) == 0 && len(b.Scores) == 0 && len(b.Steps) == 0
}

type judgeDirection struct {
	judge     string
	direction RelevanceDirection
}

// RelevanceLedger is the folded verdict ledger: one reader record per pair
// and the highest score per pair, judge and direction. Folding is a join, so
// a ledger read from segments in any order, with duplicates, is the same.
type RelevanceLedger struct {
	readers map[RelevancePair]ReaderVerdict
	scores  map[RelevancePair]map[judgeDirection]uint8
	steps   []DriftStep
}

// NewRelevanceLedger folds batches into a ledger.
func NewRelevanceLedger(batches ...RelevanceBatch) *RelevanceLedger {
	l := &RelevanceLedger{readers: map[RelevancePair]ReaderVerdict{}, scores: map[RelevancePair]map[judgeDirection]uint8{}}
	for _, batch := range batches {
		l.Add(batch)
	}
	return l
}

// Add joins a batch into the ledger.
func (l *RelevanceLedger) Add(batch RelevanceBatch) {
	for _, record := range batch.Readers {
		l.readers[record.Pair] = l.readers[record.Pair].Join(record.Verdict)
	}
	for _, record := range batch.Scores {
		byJudge := l.scores[record.Pair]
		if byJudge == nil {
			byJudge = map[judgeDirection]uint8{}
			l.scores[record.Pair] = byJudge
		}
		key := judgeDirection{record.Score.Judge, record.Score.Direction}
		if current, ok := byJudge[key]; !ok || record.Score.Score > current {
			byJudge[key] = record.Score.Score
		}
	}
	for _, step := range batch.Steps {
		if !slices.ContainsFunc(l.steps, func(s DriftStep) bool { return compareDriftSteps(s, step) == 0 }) {
			l.steps = append(l.steps, step)
		}
	}
}

// Reader returns the reader record of a pair.
func (l *RelevanceLedger) Reader(pair RelevancePair) (ReaderVerdict, bool) {
	v, ok := l.readers[pair]
	return v, ok
}

// Score returns judge's score of pair from subject to the other member: a
// both-way score, else the one-way score in that direction.
func (l *RelevanceLedger) Score(pair RelevancePair, subject, judge string) (uint8, bool) {
	byJudge := l.scores[pair]
	if score, ok := byJudge[judgeDirection{judge, DirectionBoth}]; ok {
		return score, true
	}
	score, ok := byJudge[judgeDirection{judge, DirectionFrom(pair, subject)}]
	return score, ok
}

// Batch returns the ledger's content as one canonical batch: records sorted
// by pair, judge and direction, so equal ledgers give equal batches.
func (l *RelevanceLedger) Batch() RelevanceBatch {
	var batch RelevanceBatch
	for _, pair := range sortedPairs(slices.Collect(maps.Keys(l.readers))) {
		batch.Readers = append(batch.Readers, ReaderRecord{Pair: pair, Verdict: l.readers[pair]})
	}
	for _, pair := range sortedPairs(slices.Collect(maps.Keys(l.scores))) {
		keys := slices.Collect(maps.Keys(l.scores[pair]))
		slices.SortFunc(keys, func(x, y judgeDirection) int {
			if c := strings.Compare(x.judge, y.judge); c != 0 {
				return c
			}
			return int(x.direction) - int(y.direction)
		})
		for _, key := range keys {
			batch.Scores = append(batch.Scores, ScoreRecord{Pair: pair, Score: FilterScore{Judge: key.judge, Direction: key.direction, Score: l.scores[pair][key]}})
		}
	}
	batch.Steps = slices.Clone(l.steps)
	slices.SortFunc(batch.Steps, compareDriftSteps)
	return batch
}

// Pairs returns every pair holding a reader record, sorted.
func (l *RelevanceLedger) Pairs() []RelevancePair {
	return sortedPairs(slices.Collect(maps.Keys(l.readers)))
}

func sortedPairs(pairs []RelevancePair) []RelevancePair {
	slices.SortFunc(pairs, func(x, y RelevancePair) int {
		if c := strings.Compare(x.A, y.A); c != 0 {
			return c
		}
		return strings.Compare(x.B, y.B)
	})
	return pairs
}

// RelevanceDecision is what the ledger says of a pair.
type RelevanceDecision uint8

const (
	// RelevanceUndecided leaves the pair to the prefilter or the agent.
	RelevanceUndecided RelevanceDecision = iota
	RelevanceRelevant
	RelevanceNotRelevant
)

// RelevanceThresholds are a judge's calibrated limits: a candidate scoring
// below SetAside is set aside, one scoring at or above Relevant is served in
// full. Valid is false until the recall bound holds (D33), and an invalid
// set decides nothing.
type RelevanceThresholds struct {
	Valid    bool
	SetAside float64
	Relevant float64
}

// Lookup decides a pair from subject to candidate (5.7): a reader record
// decides; else judge's score against the thresholds. A one-way score in
// the other direction answers only when it rates the pair relevant.
func (l *RelevanceLedger) Lookup(subject, candidate, judge string, thresholds RelevanceThresholds) RelevanceDecision {
	pair := NewRelevancePair(subject, candidate)
	if reader, ok := l.readers[pair]; ok && reader.Code != ReaderNone {
		if reader.Code == ReaderReferenced {
			return RelevanceRelevant
		}
		return RelevanceNotRelevant
	}
	if !thresholds.Valid || judge == "" {
		return RelevanceUndecided
	}
	if score, ok := l.Score(pair, subject, judge); ok {
		return thresholds.decide(ScoreValue(score))
	}
	if score, ok := l.scores[pair][judgeDirection{judge, DirectionFrom(pair, candidate)}]; ok {
		if decision := thresholds.decide(ScoreValue(score)); decision == RelevanceRelevant {
			return decision
		}
	}
	return RelevanceUndecided
}

func (t RelevanceThresholds) decide(score float64) RelevanceDecision {
	switch {
	case score >= t.Relevant:
		return RelevanceRelevant
	case score < t.SetAside:
		return RelevanceNotRelevant
	}
	return RelevanceUndecided
}

// DriftStep records that the audit drift rule lowered a judge's set-aside
// threshold to SetAside at time At (D36). Steps only ever lower it.
type DriftStep struct {
	Judge    string
	At       time.Time
	SetAside uint8
}

func compareDriftSteps(x, y DriftStep) int {
	if c := strings.Compare(x.Judge, y.Judge); c != 0 {
		return c
	}
	if c := x.At.Compare(y.At); c != 0 {
		return c
	}
	return int(x.SetAside) - int(y.SetAside)
}
