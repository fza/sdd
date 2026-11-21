package relevance_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// memorySizes is relevance.MemoryBatchSizes failing every Lower and Raise
// with err.
type memorySizes struct {
	relevance.MemoryBatchSizes
	err error
}

func (m *memorySizes) Lower(key string, size int) error {
	if m.err != nil {
		return m.err
	}
	return m.MemoryBatchSizes.Lower(key, size)
}

func (m *memorySizes) Raise(key string, from, size int) error {
	if m.err != nil {
		return m.err
	}
	return m.MemoryBatchSizes.Raise(key, from, size)
}

func remembering(key string, size int) *memorySizes {
	sizes := &memorySizes{}
	_ = sizes.MemoryBatchSizes.Lower(key, size)
	return sizes
}

// brittleScorer answers a batch of at most limit candidates; a larger one
// fails with fail, or, without fail, is answered one score short.
type brittleScorer struct {
	fixedScorer
	limit int
	fail  error
}

func (b *brittleScorer) Score(ctx context.Context, request relevance.Request) (relevance.Result, error) {
	result, err := b.fixedScorer.Score(ctx, request)
	if err != nil || len(request.Candidates) <= b.limit {
		return result, err
	}
	if b.fail != nil {
		return relevance.Result{}, b.fail
	}
	result.Scores = result.Scores[1:]
	return result, nil
}

func (b *brittleScorer) sizes() []int {
	var sizes []int
	for _, request := range b.requests {
		sizes = append(sizes, len(request.Candidates))
	}
	return sizes
}

func manyCandidates(n int) []relevance.Candidate {
	out := make([]relevance.Candidate, n)
	for i := range out {
		out[i] = relevance.Candidate{ID: string(rune('a' + i%26)), Text: string(make([]byte, i%7))}
	}
	return out
}

const halvingKey = "chat:haiku"

// A broken answer halves the batch until one holds, every candidate gets
// its score in request order, and the next request starts at the half that
// worked.
func TestHalving_HalvesABrokenBatchAndRemembersTheSize(t *testing.T) {
	for name, fail := range map[string]error{
		"malformed":  &llm.Error{Err: &llm.MalformedAnswerError{Text: "{", Err: errors.New("unparsable")}},
		"incomplete": nil,
	} {
		t.Run(name, func(t *testing.T) {
			inner := &brittleScorer{fixedScorer: fixedScorer{maxBatch: 100}, limit: 25, fail: fail}
			sizes := &memorySizes{}
			scorer := relevance.Halving(inner, sizes, halvingKey)
			request := relevance.Request{Subject: "draft", Candidates: manyCandidates(100)}

			result, err := scorer.Score(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if err := relevance.CheckResult(request, result); err != nil {
				t.Fatal(err)
			}
			for i, candidate := range request.Candidates {
				if want := float64(len(candidate.Text)) / 100; result.Scores[i] != want {
					t.Fatalf("score %d = %v, want %v: scores should stay in request order", i, result.Scores[i], want)
				}
			}
			if want := []int{100, 50, 25, 25, 25, 25}; !slices.Equal(inner.sizes(), want) {
				t.Errorf("calls of %v, want %v", inner.sizes(), want)
			}
			if result.Usage.InputTokens != 40 {
				t.Errorf("usage should sum the calls that answered, got %d input tokens", result.Usage.InputTokens)
			}
			if size, _ := sizes.Size(halvingKey); size != 25 || scorer.MaxBatch() != 25 {
				t.Errorf("remembered %d, MaxBatch %d: the next request should start at 25", size, scorer.MaxBatch())
			}
		})
	}
}

// A remembered size is where the next run starts.
func TestHalving_StartsAtTheRememberedSize(t *testing.T) {
	inner := &brittleScorer{fixedScorer: fixedScorer{maxBatch: 100}, limit: 100}
	scorer := relevance.Halving(inner, remembering(halvingKey, 12), halvingKey)
	if scorer.MaxBatch() != 12 {
		t.Fatalf("MaxBatch = %d, want the remembered 12", scorer.MaxBatch())
	}
	if _, err := scorer.Score(t.Context(), relevance.Request{Candidates: manyCandidates(30)}); err != nil {
		t.Fatal(err)
	}
	if want := []int{12, 12, 6}; !slices.Equal(inner.sizes(), want) {
		t.Errorf("calls of %v, want %v", inner.sizes(), want)
	}
	other := relevance.Halving(inner, &memorySizes{}, halvingKey)
	if other.MaxBatch() != 100 {
		t.Errorf("MaxBatch = %d, want the inner 100 while nothing is remembered", other.MaxBatch())
	}
}

// A call past its own deadline halves; the caller's deadline ending the
// pass does not.
func TestHalving_HalvesABatchThatTimesOut(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := &slowScorer{limit: 10}
		sizes := &memorySizes{}
		scorer := relevance.Halving(relevance.Bounded(inner, time.Second), sizes, halvingKey)
		if _, err := scorer.Score(t.Context(), relevance.Request{Candidates: manyCandidates(40)}); err != nil {
			t.Fatal(err)
		}
		if size, _ := sizes.Size(halvingKey); size != 10 {
			t.Errorf("remembered %d, want 10 after two timeouts", size)
		}

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		ended := &memorySizes{}
		_, err := relevance.Halving(inner, ended, halvingKey).Score(ctx, relevance.Request{Candidates: manyCandidates(40)})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("the caller's deadline should end the request, got %v", err)
		}
		if _, ok := ended.Size(halvingKey); ok {
			t.Error("the caller's deadline should remember no size")
		}
	})
}

// slowScorer answers a batch of at most limit candidates at once and holds
// a larger one until its context ends.
type slowScorer struct {
	limit int
}

func (s *slowScorer) Direction() relevance.Direction { return relevance.Both }
func (s *slowScorer) MaxBatch() int                  { return 100 }
func (s *slowScorer) MaxInputTokens() int            { return 0 }
func (s *slowScorer) MaxPairTokens() int             { return 0 }

func (s *slowScorer) Score(ctx context.Context, request relevance.Request) (relevance.Result, error) {
	if len(request.Candidates) > s.limit {
		<-ctx.Done()
		return relevance.Result{}, ctx.Err()
	}
	return relevance.Result{Scores: make([]float64, len(request.Candidates))}, nil
}

// A failure a smaller batch would repeat, one of a single candidate, and a
// size that cannot be remembered or raised fail the request.
func TestHalving_FailsWhatHalvingCannotMend(t *testing.T) {
	down := errors.New("connection refused")
	inner := &brittleScorer{fixedScorer: fixedScorer{maxBatch: 100}, limit: 0, fail: down}
	sizes := &memorySizes{}
	_, err := relevance.Halving(inner, sizes, halvingKey).Score(t.Context(), relevance.Request{Candidates: manyCandidates(8)})
	if !errors.Is(err, down) || len(inner.requests) != 1 {
		t.Fatalf("%d calls, error %v: a provider failure should fail at once", len(inner.requests), err)
	}
	if _, ok := sizes.Size(halvingKey); ok {
		t.Error("a provider failure should remember no size")
	}

	broken := &llm.MalformedAnswerError{Err: errors.New("unparsable")}
	single := &brittleScorer{fixedScorer: fixedScorer{maxBatch: 100, err: broken}}
	_, err = relevance.Halving(single, &memorySizes{}, halvingKey).Score(t.Context(), relevance.Request{Candidates: manyCandidates(4)})
	if !errors.Is(err, broken) {
		t.Fatalf("a single candidate's broken answer should fail the request, got %v", err)
	}
	if want := []int{4, 2, 1}; !slices.Equal(single.sizes(), want) {
		t.Errorf("calls of %v, want %v", single.sizes(), want)
	}

	unstored := errors.New("disk full")
	brittle := &brittleScorer{fixedScorer: fixedScorer{maxBatch: 100}, limit: 1, fail: broken}
	_, err = relevance.Halving(brittle, &memorySizes{err: unstored}, halvingKey).Score(t.Context(), relevance.Request{Candidates: manyCandidates(4)})
	if !errors.Is(err, unstored) {
		t.Fatalf("a size that cannot be remembered should fail the request, got %v", err)
	}

	unraised := remembering(halvingKey, 4)
	unraised.err = unstored
	_, err = relevance.Halving(&brittleScorer{fixedScorer: fixedScorer{maxBatch: 100}, limit: 100}, unraised, halvingKey).Score(t.Context(), relevance.Request{Candidates: manyCandidates(4)})
	if !errors.Is(err, unstored) {
		t.Fatalf("a size that cannot be raised should fail the request, got %v", err)
	}
}

// A request whose calls all answer, one of them at the remembered size,
// doubles that size up to the inner MaxBatch, so a failure that passed does
// not pin it; a request that halved leaves the half standing.
func TestHalving_GrowsBackOnceTheRememberedSizeHolds(t *testing.T) {
	cases := []struct {
		name       string
		remembered int
		// limit is the largest batch the inner scorer answers.
		limit int
		// requests are the candidates of each request in turn, after the
		// size remembered once each returned, 0 for none.
		requests []int
		after    []int
	}{
		{"a clean request at the remembered size doubles it", 12, 100, []int{30}, []int{24}},
		{"each clean request takes one step", 12, 100, []int{30, 60, 60}, []int{24, 48, 96}},
		{"doubling stops at the inner MaxBatch", 60, 100, []int{100, 100}, []int{100, 100}},
		{"a request smaller than the remembered size proves nothing", 12, 100, []int{8}, []int{12}},
		{"a request that halved leaves the half standing", 8, 4, []int{16}, []int{4}},
		{"a size that fails again halves again", 4, 4, []int{8, 8, 8}, []int{8, 4, 8}},
		{"nothing remembered leaves nothing to grow", 0, 100, []int{100}, []int{0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner := &brittleScorer{fixedScorer: fixedScorer{maxBatch: 100}, limit: tc.limit, fail: &llm.MalformedAnswerError{Err: errors.New("unparsable")}}
			sizes := &memorySizes{}
			if tc.remembered > 0 {
				sizes = remembering(halvingKey, tc.remembered)
			}
			scorer := relevance.Halving(inner, sizes, halvingKey)
			for i, n := range tc.requests {
				request := relevance.Request{Candidates: manyCandidates(n)}
				result, err := scorer.Score(t.Context(), request)
				if err != nil {
					t.Fatal(err)
				}
				if err := relevance.CheckResult(request, result); err != nil {
					t.Fatal(err)
				}
				if size, _ := sizes.Size(halvingKey); size != tc.after[i] {
					t.Fatalf("request %d of %d candidates left size %d, want %d (calls of %v)", i+1, n, size, tc.after[i], inner.sizes())
				}
			}
		})
	}
}

// Raise lands only on the size still held.
func TestMemoryBatchSizes_RaisesTheSizeStillHeld(t *testing.T) {
	cases := []struct {
		name string
		held int
		from int
		want int
	}{
		{"the size still held", 4, 4, 8},
		{"a size lowered meanwhile", 2, 4, 2},
		{"no size held", 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sizes := &relevance.MemoryBatchSizes{}
			if tc.held > 0 {
				if err := sizes.Lower(halvingKey, tc.held); err != nil {
					t.Fatal(err)
				}
			}
			if err := sizes.Raise(halvingKey, tc.from, 8); err != nil {
				t.Fatal(err)
			}
			if got, _ := sizes.Size(halvingKey); got != tc.want {
				t.Errorf("size %d, want %d: a raise should land only on the size it answered at", got, tc.want)
			}
		})
	}
}

// meddlingScorer runs meanwhile before its call number at, as another
// request sharing the sizes would.
type meddlingScorer struct {
	relevance.Scorer
	calls     int
	at        int
	meanwhile func()
}

func (m *meddlingScorer) Score(ctx context.Context, request relevance.Request) (relevance.Result, error) {
	m.calls++
	if m.calls == m.at {
		m.meanwhile()
	}
	return m.Scorer.Score(ctx, request)
}

// A request one batch of which broke raises nothing, even where another
// request raised the size back to the one it started at.
func TestHalving_ARequestThatBrokeRaisesNothing(t *testing.T) {
	sizes := remembering(halvingKey, 8)
	inner := &brittleScorer{fixedScorer: fixedScorer{maxBatch: 100}, limit: 4, fail: &llm.MalformedAnswerError{Err: errors.New("unparsable")}}
	other := &meddlingScorer{Scorer: inner, at: 5, meanwhile: func() { _ = sizes.Raise(halvingKey, 4, 8) }}
	if _, err := relevance.Halving(other, sizes, halvingKey).Score(t.Context(), relevance.Request{Candidates: manyCandidates(16)}); err != nil {
		t.Fatal(err)
	}
	if want := []int{8, 4, 4, 4, 4}; !slices.Equal(inner.sizes(), want) {
		t.Fatalf("calls of %v, want %v", inner.sizes(), want)
	}
	if size, _ := sizes.Size(halvingKey); size != 8 {
		t.Errorf("size %d: the request that broke at 8 should leave the other request's 8 standing", size)
	}
}
