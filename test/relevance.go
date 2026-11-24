package test

import (
	"context"
	_ "embed"
	"sync"

	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// SegmentBeforeSource is a relevance ledger segment encoded before reader
// records carried their label source: its reader flags hold the audit bit
// alone. It holds the batch of the relevance codec test's sampleBatch.
//
//go:embed fixtures/relevance/before-source.seg
var SegmentBeforeSource []byte

// ScriptedScorer is a relevance scorer answering Scores per candidate ID,
// Default for the rest, rating both directions. It records every request.
type ScriptedScorer struct {
	Scores  map[string]float64
	Default float64
	// Batch is the most candidates one call takes; zero for no limit.
	Batch int
	// Script, when set, runs ahead of each call's answer, where it may wait;
	// an error it returns fails the call.
	Script func(context.Context, relevance.Request) error

	mu       sync.Mutex
	requests []relevance.Request
}

func (s *ScriptedScorer) Direction() relevance.Direction { return relevance.Both }
func (s *ScriptedScorer) MaxBatch() int                  { return s.Batch }
func (s *ScriptedScorer) MaxInputTokens() int            { return 0 }
func (s *ScriptedScorer) MaxPairTokens() int             { return 0 }
func (s *ScriptedScorer) Reads(relevance.Candidate) bool { return true }

func (s *ScriptedScorer) Score(ctx context.Context, request relevance.Request) (relevance.Result, error) {
	s.mu.Lock()
	s.requests = append(s.requests, request)
	s.mu.Unlock()
	if s.Script != nil {
		err := s.Script(ctx, request)
		if err != nil {
			return relevance.Result{}, err
		}
	}
	result := relevance.Result{Identity: llm.Identity{Provider: "test", Model: "scripted"}}
	for _, candidate := range request.Candidates {
		score, ok := s.Scores[candidate.ID]
		if !ok {
			score = s.Default
		}
		result.Scores = append(result.Scores, score)
	}
	return result, nil
}

// Scored returns the candidate IDs every request so far asked about, in
// order, repeats included.
func (s *ScriptedScorer) Scored() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for _, request := range s.requests {
		for _, candidate := range request.Candidates {
			ids = append(ids, candidate.ID)
		}
	}
	return ids
}
