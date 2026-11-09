// Package relevance is the public port of the grounding prefilter
// (20261109-004506-d-cpt-udz, D23): a Scorer rates how relevant each
// candidate text is to one subject text, a batch per call. Adapters plug in
// a provider protocol; the decorators mirror pkg/llm's. Like llm.Runner, a
// Scorer keeps no internal retries and bounds no call of its own: deadlines
// and rates are the host's composition.
package relevance

import (
	"context"
	"fmt"

	"github.com/networkteam/sdd/pkg/llm"
)

// Purpose names prefilter calls in the stats a sink records.
const Purpose = "relevance"

// Direction is which way a Scorer's verdict answers.
type Direction int

const (
	// Both is a judge that reads subject and candidate alike: its score
	// answers the pair either way.
	Both Direction = iota
	// Forward is a one-way judge (a reranker): its score answers only from
	// the subject to the candidate.
	Forward
)

// Candidate is one text to rate against the subject, named by the caller.
type Candidate struct {
	ID   string
	Text string
}

// Request asks for the relevance of every candidate to the subject.
type Request struct {
	Subject    string
	Candidates []Candidate
}

// Result carries one score in [0, 1] per candidate, in request order, and
// what served the call.
type Result struct {
	Scores   []float64
	Identity llm.Identity
	Usage    llm.Usage
}

// Scorer is the port. MaxBatch bounds the candidates one call takes, 0 for
// no bound; MaxInputTokens bounds the subject and candidates of one call
// together, 0 for no bound, so a caller fills each call up to the
// provider's limit without cutting any text. MaxPairTokens bounds the
// subject and one candidate together, 0 for no bound: a reranker reads each
// document beside the query in a window of its own.
type Scorer interface {
	Score(ctx context.Context, request Request) (Result, error)
	Direction() Direction
	MaxBatch() int
	MaxInputTokens() int
	MaxPairTokens() int
}

// EstimateTokens is a conservative token estimate of text: three bytes per
// token, so a filled call stays under a provider's limit.
func EstimateTokens(text string) int { return (len(text) + 2) / 3 }

// CheckResult reports a result that does not hold one score in [0, 1] per
// candidate.
func CheckResult(request Request, result Result) error {
	if len(result.Scores) != len(request.Candidates) {
		return fmt.Errorf("relevance: %d scores for %d candidates", len(result.Scores), len(request.Candidates))
	}
	for i, score := range result.Scores {
		if score < 0 || score > 1 || score != score {
			return fmt.Errorf("relevance: score %v of candidate %s lies outside [0, 1]", score, request.Candidates[i].ID)
		}
	}
	return nil
}

// Plan splits candidates into calls for scorer: each call within MaxBatch
// and MaxInputTokens, counting the subject once per call. A candidate too
// long for a call on its own, or beside the subject past MaxPairTokens, lands
// in oversize instead, by index; nothing is cut (D30). Batches hold candidate
// indexes in request order.
func Plan(scorer Scorer, subject string, candidates []Candidate) (batches [][]int, oversize []int) {
	maxBatch, maxTokens, maxPair := scorer.MaxBatch(), scorer.MaxInputTokens(), scorer.MaxPairTokens()
	subjectTokens := EstimateTokens(subject)
	var current []int
	tokens := subjectTokens
	for i, candidate := range candidates {
		cost := EstimateTokens(candidate.Text)
		if maxTokens > 0 && subjectTokens+cost > maxTokens || maxPair > 0 && subjectTokens+cost > maxPair {
			oversize = append(oversize, i)
			continue
		}
		full := maxBatch > 0 && len(current) == maxBatch
		over := maxTokens > 0 && tokens+cost > maxTokens
		if len(current) > 0 && (full || over) {
			batches = append(batches, current)
			current, tokens = nil, subjectTokens
		}
		current = append(current, i)
		tokens += cost
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches, oversize
}
