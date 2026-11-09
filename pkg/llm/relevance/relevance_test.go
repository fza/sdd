package relevance_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// fixedScorer scores every candidate at its length divided by 100 and
// records each request.
type fixedScorer struct {
	mu        sync.Mutex
	maxBatch  int
	maxTokens int
	maxPair   int
	requests  []relevance.Request
	err       error
	block     bool
}

func (f *fixedScorer) Direction() relevance.Direction { return relevance.Both }
func (f *fixedScorer) MaxBatch() int                  { return f.maxBatch }
func (f *fixedScorer) MaxInputTokens() int            { return f.maxTokens }
func (f *fixedScorer) MaxPairTokens() int             { return f.maxPair }

func (f *fixedScorer) Score(ctx context.Context, request relevance.Request) (relevance.Result, error) {
	f.mu.Lock()
	f.requests = append(f.requests, request)
	f.mu.Unlock()
	if f.block {
		<-ctx.Done()
		return relevance.Result{}, ctx.Err()
	}
	if f.err != nil {
		return relevance.Result{}, f.err
	}
	result := relevance.Result{Identity: llm.Identity{Provider: "test", Model: "fixed"}, Usage: llm.Usage{InputTokens: 10, CostUSD: 0.5}}
	for _, candidate := range request.Candidates {
		result.Scores = append(result.Scores, float64(len(candidate.Text))/100)
	}
	return result, nil
}

func candidates(texts ...string) []relevance.Candidate {
	out := make([]relevance.Candidate, len(texts))
	for i, text := range texts {
		out[i] = relevance.Candidate{ID: string(rune('a' + i)), Text: text}
	}
	return out
}

func TestPlan(t *testing.T) {
	long := strings.Repeat("x", 300)
	cases := []struct {
		name      string
		maxBatch  int
		maxTokens int
		maxPair   int
		texts     []string
		batches   [][]int
		oversize  []int
	}{
		{"no limits take one call", 0, 0, 0, []string{"a", "b", "c"}, [][]int{{0, 1, 2}}, nil},
		{"the batch limit splits calls", 2, 0, 0, []string{"a", "b", "c"}, [][]int{{0, 1}, {2}}, nil},
		{"the token limit fills each call", 0, 110, 0, []string{long, long, "a"}, [][]int{{0}, {1, 2}}, nil},
		{"a candidate too long alone is left out, never cut", 0, 50, 0, []string{"a", long, "b"}, [][]int{{0, 2}}, []int{1}},
		{"a pair past the pair window is left out, never cut", 0, 0, 50, []string{"a", long, "b"}, [][]int{{0, 2}}, []int{1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scorer := &fixedScorer{maxBatch: tc.maxBatch, maxTokens: tc.maxTokens, maxPair: tc.maxPair}
			batches, oversize := relevance.Plan(scorer, "subject", candidates(tc.texts...))
			if !reflect.DeepEqual(batches, tc.batches) || !reflect.DeepEqual(oversize, tc.oversize) {
				t.Fatalf("Plan = %v, oversize %v; want %v, oversize %v", batches, oversize, tc.batches, tc.oversize)
			}
		})
	}
}

func TestPerItem_FansABatchOutOneCandidatePerCall(t *testing.T) {
	inner := &fixedScorer{maxBatch: 1, maxPair: 5}
	scorer := relevance.PerItem(inner, 4)
	if scorer.MaxBatch() != 0 || scorer.MaxPairTokens() != 5 {
		t.Fatal("a per-item scorer should take a batch of any size and keep the pair window")
	}
	result, err := scorer.Score(t.Context(), relevance.Request{Subject: "s", Candidates: candidates("a", "bb", "ccc")})
	if err != nil {
		t.Fatal(err)
	}
	if len(inner.requests) != 3 {
		t.Fatalf("%d inner calls: each candidate should get its own", len(inner.requests))
	}
	for _, request := range inner.requests {
		if len(request.Candidates) != 1 || request.Subject != "s" {
			t.Fatalf("inner request %+v should hold the subject and one candidate", request)
		}
	}
	if !reflect.DeepEqual(result.Scores, []float64{0.01, 0.02, 0.03}) {
		t.Fatalf("scores %v should keep request order", result.Scores)
	}
	if result.Usage.InputTokens != 30 || result.Usage.CostUSD != 1.5 {
		t.Fatalf("usage %+v should sum every call", result.Usage)
	}

	failing := relevance.PerItem(&fixedScorer{err: errors.New("down")}, 2)
	if _, err := failing.Score(t.Context(), relevance.Request{Candidates: candidates("a", "b")}); err == nil {
		t.Fatal("a failed call should fail the batch")
	}
}

func TestBounded_EndsACallAtItsDeadline(t *testing.T) {
	scorer := relevance.Bounded(&fixedScorer{block: true, maxBatch: 3, maxTokens: 9, maxPair: 5}, 10*time.Millisecond)
	if scorer.MaxBatch() != 3 || scorer.MaxInputTokens() != 9 || scorer.MaxPairTokens() != 5 {
		t.Fatal("a decorator should keep the inner scorer's limits")
	}
	if _, err := scorer.Score(t.Context(), relevance.Request{Candidates: candidates("a")}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Score = %v, the deadline should end the call", err)
	}
}

type statSink struct {
	stats []llm.CallStat
}

func (s *statSink) RecordCall(_ context.Context, stat llm.CallStat) { s.stats = append(s.stats, stat) }

func TestObserved_RecordsEveryCall(t *testing.T) {
	sink := &statSink{}
	scorer := relevance.Observed(&fixedScorer{}, sink)
	if _, err := scorer.Score(t.Context(), relevance.Request{Candidates: candidates("a", "b")}); err != nil {
		t.Fatal(err)
	}
	failing := relevance.Observed(&fixedScorer{err: &llm.Error{Identity: llm.Identity{Model: "m"}, Err: errors.New("down")}}, sink)
	if _, err := failing.Score(t.Context(), relevance.Request{Candidates: candidates("a")}); err == nil {
		t.Fatal("the failure should pass through")
	}
	if len(sink.stats) != 2 {
		t.Fatalf("%d stats: every call, failed or not, should be recorded", len(sink.stats))
	}
	if sink.stats[0].Purpose != relevance.Purpose || sink.stats[0].Items != 2 || sink.stats[0].Identity.Model != "fixed" {
		t.Fatalf("stat %+v should carry the purpose, the candidate count and the identity", sink.stats[0])
	}
	if sink.stats[1].Error == "" || sink.stats[1].Identity.Model != "m" {
		t.Fatalf("stat %+v should carry the failure and its attributed identity", sink.stats[1])
	}
}

func TestCheckResult(t *testing.T) {
	request := relevance.Request{Candidates: candidates("a", "b")}
	for name, scores := range map[string][]float64{"too few": {0.5}, "above one": {0.5, 1.5}, "below zero": {-0.1, 0.2}} {
		if err := relevance.CheckResult(request, relevance.Result{Scores: scores}); err == nil {
			t.Errorf("%s: scores %v should be refused", name, scores)
		}
	}
	if err := relevance.CheckResult(request, relevance.Result{Scores: []float64{0, 1}}); err != nil {
		t.Fatal(err)
	}
}
