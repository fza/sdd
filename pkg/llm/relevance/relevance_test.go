package relevance_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
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
	// entered, when set, takes a signal per call, which then waits for
	// release.
	entered chan struct{}
	release chan struct{}
}

func (f *fixedScorer) Direction() relevance.Direction { return relevance.Both }
func (f *fixedScorer) MaxBatch() int                  { return f.maxBatch }
func (f *fixedScorer) MaxInputTokens() int            { return f.maxTokens }
func (f *fixedScorer) MaxPairTokens() int             { return f.maxPair }

func (f *fixedScorer) Score(ctx context.Context, request relevance.Request) (relevance.Result, error) {
	f.mu.Lock()
	f.requests = append(f.requests, request)
	f.mu.Unlock()
	if f.entered != nil {
		f.entered <- struct{}{}
		<-f.release
	}
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

func TestBounded_EndsACallAtItsDeadline(t *testing.T) {
	scorer := relevance.Bounded(&fixedScorer{block: true, maxBatch: 3, maxTokens: 9, maxPair: 5}, 10*time.Millisecond)
	if scorer.MaxBatch() != 3 || scorer.MaxInputTokens() != 9 || scorer.MaxPairTokens() != 5 {
		t.Fatal("a decorator should keep the inner scorer's limits")
	}
	if _, err := scorer.Score(t.Context(), relevance.Request{Candidates: candidates("a")}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Score = %v, the deadline should end the call", err)
	}
}

// delayError asks for a delay before the next call, as a 429 does.
type delayError time.Duration

func (e delayError) Error() string             { return "rate limited" }
func (e delayError) RetryDelay() time.Duration { return time.Duration(e) }

// failingOnce fails its first call with err, then scores as fixedScorer.
type failingOnce struct {
	fixedScorer
	err   error
	calls int
}

func (f *failingOnce) Score(ctx context.Context, request relevance.Request) (relevance.Result, error) {
	f.calls++
	if f.calls == 1 {
		return relevance.Result{}, f.err
	}
	return f.fixedScorer.Score(ctx, request)
}

// Calls that hit a 429 together spread their retries over a quarter of the
// delay instead of coming back in lockstep.
func TestRetriedAfterDelay_SpreadsRetriesOfTheSameDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		var mu sync.Mutex
		var waits []time.Duration
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				inner := &failingOnce{err: &llm.Error{Err: delayError(time.Second)}}
				_, err := relevance.RetriedAfterDelay(inner, time.Minute).Score(t.Context(), relevance.Request{Candidates: candidates("a")})
				if err != nil {
					t.Error(err)
				}
				mu.Lock()
				waits = append(waits, time.Since(start))
				mu.Unlock()
			})
		}
		wg.Wait()
		slices.Sort(waits)
		if waits[0] < time.Second || waits[len(waits)-1] > 1250*time.Millisecond || waits[0] == waits[len(waits)-1] {
			t.Fatalf("waits %v: every retry should wait its delay plus up to a quarter more, not all the same", waits)
		}
	})
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		value string
		want  time.Duration
	}{
		{"7", 7 * time.Second},
		{now.Add(30 * time.Second).Format(http.TimeFormat), 30 * time.Second},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0},
		{now.Add(2 * time.Hour).Format(http.TimeFormat), relevance.MaxRetryAfter},
		{"99999999999", relevance.MaxRetryAfter},
		{"-3", 0},
		{"soon", 0},
		{"", 0},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			got := relevance.ParseRetryAfter(tc.value, now)
			if got != tc.want {
				t.Fatalf("ParseRetryAfter = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRetriedAfterDelay(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		maxWait  time.Duration
		deadline time.Duration
		calls    int
	}{
		{"a delay within bounds calls once more", delayError(5 * time.Millisecond), time.Second, time.Second, 2},
		{"a delay past maxWait ends the call at once", delayError(400 * time.Millisecond), 10 * time.Millisecond, time.Second, 1},
		{"a delay past the deadline ends the call at once", delayError(time.Second), 2 * time.Second, 400 * time.Millisecond, 1},
		{"a failure without a delay ends the call", errors.New("down"), time.Second, time.Second, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner := &failingOnce{err: &llm.Error{Err: tc.err}}
			ctx, cancel := context.WithTimeout(t.Context(), tc.deadline)
			defer cancel()
			start := time.Now()
			_, err := relevance.RetriedAfterDelay(inner, tc.maxWait).Score(ctx, relevance.Request{Candidates: candidates("a")})
			elapsed := time.Since(start)
			if inner.calls != tc.calls || (tc.calls == 2) != (err == nil) || tc.calls == 1 && elapsed > 200*time.Millisecond {
				t.Fatalf("%d calls after %s, error %v: the call should be retried only after a delay it can wait out, else end at once", inner.calls, elapsed, err)
			}
		})
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

// Limited holds the calls in flight at its bound across every request and
// keeps the inner scorer's limits.
func TestLimited_HoldsTheCallsInFlight(t *testing.T) {
	inner := &fixedScorer{maxBatch: 100, entered: make(chan struct{}, 8), release: make(chan struct{})}
	scorer := relevance.Limited(inner, 2)
	if scorer.MaxBatch() != 100 {
		t.Fatal("Limited should keep the inner scorer's limits")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, _ = scorer.Score(t.Context(), relevance.Request{Subject: "s", Candidates: candidates("a")})
		})
	}
	<-inner.entered
	<-inner.entered
	select {
	case <-inner.entered:
		t.Error("a third call should wait for a slot")
	case <-time.After(50 * time.Millisecond):
	}
	close(inner.release)
	wg.Wait()
}
