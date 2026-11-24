package relevance_test

import (
	"context"
	"errors"
	"fmt"
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

func (m *memorySizes) Broke(key string, size int) error {
	if m.err != nil {
		return m.err
	}
	return m.MemoryBatchSizes.Broke(key, size)
}

func (m *memorySizes) Raise(key string, from, size int) error {
	if m.err != nil {
		return m.err
	}
	return m.MemoryBatchSizes.Raise(key, from, size)
}

func remembering(size int) *memorySizes {
	sizes := &memorySizes{}
	_ = sizes.MemoryBatchSizes.Lower(halvingKey, size)
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
	scorer := relevance.Halving(inner, remembering(12), halvingKey)
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
		_, err := relevance.Halving(&slowScorer{limit: 0}, ended, halvingKey).Score(ctx, relevance.Request{Candidates: manyCandidates(1)})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("the caller's deadline should end the request, got %v", err)
		}
		if _, ok := ended.Size(halvingKey); ok {
			t.Error("the caller's deadline should remember no size")
		}
	})
}

// A call limit as long as the caller's deadline still halves a batch: a
// call of several candidates gets half the time the caller has left.
func TestHalving_LeavesRoomForHalvesWithinTheCallersDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := &slowScorer{limit: 10}
		sizes := &memorySizes{}
		scorer := relevance.Halving(relevance.Bounded(inner, 45*time.Second), sizes, halvingKey)
		ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		defer cancel()
		result, err := scorer.Score(ctx, relevance.Request{Candidates: manyCandidates(40)})
		if err != nil {
			t.Fatalf("the halves should answer within the caller's deadline: %v", err)
		}
		if len(result.Scores) != 40 {
			t.Errorf("%d scores: every candidate should be scored by the halves", len(result.Scores))
		}
		if size, _ := sizes.Size(halvingKey); size != 10 {
			t.Errorf("remembered %d, want the size that answered", size)
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

	unraised := remembering(4)
	unraised.err = unstored
	_, err = relevance.Halving(&brittleScorer{fixedScorer: fixedScorer{maxBatch: 100}, limit: 100}, unraised, halvingKey).Score(t.Context(), relevance.Request{Candidates: manyCandidates(4)})
	if !errors.Is(err, unstored) {
		t.Fatalf("a size that cannot be raised should fail the request, got %v", err)
	}
}

// watchingScorer notes the size sizes holds as each call starts, fails the
// calls fail names and, while a call lower names runs, lowers the size as
// another request would; calls count from 1.
type watchingScorer struct {
	fixedScorer
	sizes *memorySizes
	fail  map[int]error
	lower map[int]int
	held  []int
}

func (w *watchingScorer) Score(ctx context.Context, request relevance.Request) (relevance.Result, error) {
	size, _ := w.sizes.Size(halvingKey)
	w.held = append(w.held, size)
	call := len(w.held)
	if lowered, ok := w.lower[call]; ok {
		_ = w.sizes.MemoryBatchSizes.Lower(halvingKey, lowered)
	}
	err := w.fail[call]
	if err != nil {
		return relevance.Result{}, err
	}
	return w.fixedScorer.Score(ctx, request)
}

// One call answered at the remembered size doubles it at once, whatever the
// request's later calls do; a call of a request that broke raises nothing.
func TestHalving_GrowsOnceACallAnswersAtTheRememberedSize(t *testing.T) {
	down := errors.New("connection refused")
	broken := &llm.MalformedAnswerError{Err: errors.New("unparsable")}
	cases := []struct {
		name       string
		remembered int
		candidates int
		far        int
		// fail is the failure of a call, lower the size another request
		// lowers to while a call runs, by the call's number from 1.
		fail  map[int]error
		lower map[int]int
		// unstorable fails every Lower and Raise the request asks for.
		unstorable bool
		err        error
		// held is the remembered size as each call starts.
		held  []int
		after int
	}{
		{name: "the first answered call doubles it while later calls are still to run", remembered: 12, candidates: 30, held: []int{12, 24, 24}, after: 24},
		{name: "a later call failing the request leaves the doubled size", remembered: 12, candidates: 30, fail: map[int]error{3: down}, err: down, held: []int{12, 24, 24}, after: 24},
		{name: "a later call that breaks halves from its own size", remembered: 12, candidates: 30, fail: map[int]error{2: broken}, held: []int{12, 24, 6, 6, 6}, after: 6},
		{name: "a first call that fails leaves the size", remembered: 12, candidates: 30, fail: map[int]error{1: down}, err: down, held: []int{12}, after: 12},
		{name: "calls after a break raise nothing", remembered: 12, candidates: 30, fail: map[int]error{1: broken}, held: []int{12, 6, 6, 6, 6, 6}, after: 6},
		{name: "a call below the remembered size raises nothing", remembered: 12, candidates: 8, held: []int{12}, after: 12},
		{name: "a size lowered while the call ran stays", remembered: 12, candidates: 12, lower: map[int]int{1: 6}, held: []int{12}, after: 6},
		{name: "a call answered at the inner MaxBatch asks for no raise", remembered: 100, candidates: 100, unstorable: true, held: []int{100}, after: 100},
		{name: "far candidates count towards the size of a call", remembered: 8, candidates: 12, far: 3, held: []int{8, 16, 16}, after: 16},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sizes := remembering(tc.remembered)
			if tc.unstorable {
				sizes.err = errors.New("disk full")
			}
			inner := &watchingScorer{fixedScorer: fixedScorer{maxBatch: 100}, sizes: sizes, fail: tc.fail, lower: tc.lower}
			request := relevance.Request{Candidates: manyCandidates(tc.candidates)}
			_, err := relevance.ScoreWithFar(t.Context(), relevance.Halving(inner, sizes, halvingKey), request, farCandidates(make([]int, tc.far)...))
			if !errors.Is(err, tc.err) {
				t.Fatalf("error %v, want %v", err, tc.err)
			}
			if !slices.Equal(inner.held, tc.held) {
				t.Errorf("calls started at the remembered sizes %v, want %v", inner.held, tc.held)
			}
			if size, _ := sizes.Size(halvingKey); size != tc.after {
				t.Errorf("the request left size %d, want %d", size, tc.after)
			}
		})
	}
}

// gatedScorer holds the first call starting with a gated candidate until
// its gate takes the call's outcome, nil for an answer; every other call
// answers at once.
type gatedScorer struct {
	fixedScorer
	entered chan struct{}
	gates   map[string]chan error
}

func (g *gatedScorer) Score(ctx context.Context, request relevance.Request) (relevance.Result, error) {
	id := request.Candidates[0].ID
	g.mu.Lock()
	gate, ok := g.gates[id]
	delete(g.gates, id)
	g.mu.Unlock()
	if ok {
		g.entered <- struct{}{}
		err := <-gate
		if err != nil {
			return relevance.Result{}, err
		}
	}
	return g.fixedScorer.Score(ctx, request)
}

// A request's first call answering at the remembered size doubles it while
// the request's next call and another request's call still run, and those,
// however they end, raise nothing more.
func TestHalving_GrowsWhileOtherCallsStillRun(t *testing.T) {
	down := errors.New("connection refused")
	cases := []struct {
		name string
		// other is how the other request's call ends, later how the
		// request's own next call does; nil for an answer.
		other, later error
		// otherErr and err are what the other request and the request
		// return.
		otherErr, err error
		after         int
	}{
		{name: "both answer", after: 24},
		{name: "the other request fails", other: down, otherErr: down, after: 24},
		{name: "the other request breaks", other: &llm.MalformedAnswerError{Err: errors.New("unparsable")}, after: 6},
		{name: "the request's next call fails", later: down, err: down, after: 24},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sizes := remembering(12)
			first, next, other := make(chan error), make(chan error), make(chan error)
			inner := &gatedScorer{fixedScorer: fixedScorer{maxBatch: 100}, entered: make(chan struct{}), gates: map[string]chan error{"request-0": first, "request-12": next, "other-0": other}}
			scorer := relevance.Halving(inner, sizes, halvingKey)
			ended := map[string]chan error{"request": make(chan error, 1), "other": make(chan error, 1)}
			for name, candidates := range map[string]int{"request": 24, "other": 12} {
				request := relevance.Request{Candidates: make([]relevance.Candidate, candidates)}
				for i := range request.Candidates {
					request.Candidates[i].ID = fmt.Sprintf("%s-%d", name, i)
				}
				go func() {
					_, err := scorer.Score(t.Context(), request)
					ended[name] <- err
				}()
			}
			<-inner.entered
			<-inner.entered

			first <- nil
			<-inner.entered
			if size, _ := sizes.Size(halvingKey); size != 24 {
				t.Fatalf("size %d with one call answered and two still running: the answer should have doubled it", size)
			}

			other <- tc.other
			if err := <-ended["other"]; !errors.Is(err, tc.otherErr) {
				t.Fatalf("the other request returned %v, want %v", err, tc.otherErr)
			}
			next <- tc.later
			if err := <-ended["request"]; !errors.Is(err, tc.err) {
				t.Fatalf("the request returned %v, want %v", err, tc.err)
			}
			if size, _ := sizes.Size(halvingKey); size != tc.after {
				t.Errorf("the calls left size %d, want %d", size, tc.after)
			}
		})
	}
}

// Request after request the remembered size doubles up to the inner
// MaxBatch, so a failure that passed does not pin it; a request that halved
// leaves the half standing.
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
		{"a request answered at the remembered size doubles it", 12, 100, []int{30}, []int{24}},
		{"each request takes one step", 12, 100, []int{30, 60, 60}, []int{24, 48, 96}},
		{"doubling stops at the inner MaxBatch", 60, 100, []int{100, 100}, []int{100, 100}},
		{"a request smaller than the remembered size proves nothing", 12, 100, []int{8}, []int{12}},
		{"a request that halved leaves the half standing", 8, 4, []int{16}, []int{4}},
		{"a size that broke is not raised to again", 4, 4, []int{8, 8, 8}, []int{8, 4, 4}},
		{"nothing remembered leaves nothing to grow", 0, 100, []int{100}, []int{0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner := &brittleScorer{fixedScorer: fixedScorer{maxBatch: 100}, limit: tc.limit, fail: &llm.MalformedAnswerError{Err: errors.New("unparsable")}}
			sizes := &memorySizes{}
			if tc.remembered > 0 {
				sizes = remembering(tc.remembered)
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

var errTooManyCalls = errors.New("too many calls")

// cappedScorer fails every call after its first max, so a request that
// would not end does.
type cappedScorer struct {
	relevance.Scorer
	calls, max int
}

func (c *cappedScorer) Score(ctx context.Context, request relevance.Request) (relevance.Result, error) {
	c.calls++
	if c.calls > c.max {
		return relevance.Result{}, errTooManyCalls
	}
	return c.Scorer.Score(ctx, request)
}

// A scorer without a batch bound takes a request in one call, the far
// candidates beside its own, as it does without Halving; a call that breaks
// halves from the request's size.
func TestHalving_TakesAnUnboundedScorersRequestInOneCall(t *testing.T) {
	cases := []struct {
		name string
		// limit is the largest batch the inner scorer answers.
		limit int
		far   int
		calls []int
		// after is the size remembered once the request returned, 0 for
		// none.
		after int
	}{
		{"a request", 100, 0, []int{40}, 0},
		{"a request with far candidates", 100, 3, []int{43}, 0},
		{"a request whose call breaks", 10, 0, []int{40, 20, 10, 10, 10, 10}, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner := &brittleScorer{limit: tc.limit, fail: &llm.MalformedAnswerError{Err: errors.New("unparsable")}}
			sizes := &memorySizes{}
			scorer := relevance.Halving(&cappedScorer{Scorer: inner, max: 10}, sizes, halvingKey)
			request := relevance.Request{Subject: "draft", Candidates: manyCandidates(40)}
			result, err := relevance.ScoreWithFar(t.Context(), scorer, request, farCandidates(make([]int, tc.far)...))
			if err != nil {
				t.Fatalf("the request should end with its scores, got %v (calls of %v)", err, inner.sizes())
			}
			if err := relevance.CheckResult(request, result); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(inner.sizes(), tc.calls) {
				t.Errorf("calls of %v, want %v", inner.sizes(), tc.calls)
			}
			if size, _ := sizes.Size(halvingKey); size != tc.after {
				t.Errorf("remembered %d, want %d", size, tc.after)
			}
		})
	}
}

// A judge failing above some size pays for a call there once per
// relevance.FailedBatchRest, not every second request.
func TestHalving_RaisesNoSizeThatBrokeUntilItRested(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := &brittleScorer{fixedScorer: fixedScorer{maxBatch: 100}, limit: 4, fail: &llm.MalformedAnswerError{Err: errors.New("unparsable")}}
		sizes := remembering(4)
		scorer := relevance.Halving(inner, sizes, halvingKey)
		steps := []struct {
			// wait is the time that passes before the request.
			wait  time.Duration
			calls []int
			after int
		}{
			{0, []int{4, 4}, 8},
			{0, []int{8, 4, 4}, 4},
			{0, []int{4, 4}, 4},
			{relevance.FailedBatchRest - time.Second, []int{4, 4}, 4},
			{time.Second, []int{4, 4}, 8},
			{0, []int{8, 4, 4}, 4},
			{0, []int{4, 4}, 4},
		}
		for i, step := range steps {
			time.Sleep(step.wait)
			inner.requests = nil
			if _, err := scorer.Score(t.Context(), relevance.Request{Candidates: manyCandidates(8)}); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(inner.sizes(), step.calls) {
				t.Errorf("request %d: calls of %v, want %v", i+1, inner.sizes(), step.calls)
			}
			if size, _ := sizes.Size(halvingKey); size != step.after {
				t.Fatalf("request %d left size %d, want %d", i+1, size, step.after)
			}
		}
	})
}

// Raise lands only on the size still held, and not on a size that broke
// until it rested.
func TestMemoryBatchSizes_RaisesTheSizeStillHeld(t *testing.T) {
	cases := []struct {
		name string
		held int
		from int
		// broke is the size of a call that broke, 0 for none, rested
		// whether relevance.FailedBatchRest passed since.
		broke  int
		rested bool
		want   int
	}{
		{name: "the size still held", held: 4, from: 4, want: 8},
		{name: "a size lowered meanwhile", held: 2, from: 4, want: 2},
		{name: "no size held", want: 0},
		{name: "a size that broke", held: 4, from: 4, broke: 8, want: 4},
		{name: "a size above one that broke", held: 4, from: 4, broke: 6, want: 4},
		{name: "a size below one that broke", held: 4, from: 4, broke: 9, want: 8},
		{name: "a size that broke and rested", held: 4, from: 4, broke: 8, rested: true, want: 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				sizes := &relevance.MemoryBatchSizes{}
				if tc.held > 0 {
					if err := sizes.Lower(halvingKey, tc.held); err != nil {
						t.Fatal(err)
					}
				}
				if tc.broke > 0 {
					if err := sizes.Broke(halvingKey, tc.broke); err != nil {
						t.Fatal(err)
					}
				}
				if tc.rested {
					time.Sleep(relevance.FailedBatchRest)
				}
				if err := sizes.Raise(halvingKey, tc.from, 8); err != nil {
					t.Fatal(err)
				}
				if got, _ := sizes.Size(halvingKey); got != tc.want {
					t.Errorf("size %d, want %d: a raise should land only on the size it answered at, short of a size that broke", got, tc.want)
				}
			})
		})
	}
}

// Of two sizes that broke the smaller bars a raise, and only its own break
// restarts its rest.
func TestMemoryBatchSizes_RemembersTheSmallestSizeThatBroke(t *testing.T) {
	cases := []struct {
		name string
		// again is the size that breaks half a rest after a call of 8 broke.
		again int
		// barred is whether a raise from 4 to 8 lands neither at once nor
		// a rest after the call of 8 broke.
		barred []bool
	}{
		{"a larger size", 16, []bool{true, false}},
		{"the same size", 8, []bool{true, true}},
		{"a smaller size", 6, []bool{true, true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				sizes := &relevance.MemoryBatchSizes{}
				if err := sizes.Broke(halvingKey, 8); err != nil {
					t.Fatal(err)
				}
				time.Sleep(relevance.FailedBatchRest / 2)
				if err := sizes.Broke(halvingKey, tc.again); err != nil {
					t.Fatal(err)
				}
				for i, barred := range tc.barred {
					time.Sleep(time.Duration(i) * relevance.FailedBatchRest / 2)
					if err := sizes.Lower(halvingKey, 4); err != nil {
						t.Fatal(err)
					}
					if err := sizes.Raise(halvingKey, 4, 8); err != nil {
						t.Fatal(err)
					}
					if got, _ := sizes.Size(halvingKey); (got == 4) != barred {
						t.Errorf("raise %d left size %d: a break should bar the raise for a rest after the smallest size broke", i+1, got)
					}
				}
			})
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
// request, once the size that broke rested, raised the size back to the one
// it started at.
func TestHalving_ARequestThatBrokeRaisesNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sizes := remembering(8)
		inner := &brittleScorer{fixedScorer: fixedScorer{maxBatch: 100}, limit: 4, fail: &llm.MalformedAnswerError{Err: errors.New("unparsable")}}
		other := &meddlingScorer{Scorer: inner, at: 5, meanwhile: func() {
			time.Sleep(relevance.FailedBatchRest)
			_ = sizes.Raise(halvingKey, 4, 8)
		}}
		if _, err := relevance.Halving(other, sizes, halvingKey).Score(t.Context(), relevance.Request{Candidates: manyCandidates(16)}); err != nil {
			t.Fatal(err)
		}
		if want := []int{8, 4, 4, 4, 4}; !slices.Equal(inner.sizes(), want) {
			t.Fatalf("calls of %v, want %v", inner.sizes(), want)
		}
		if size, _ := sizes.Size(halvingKey); size != 8 {
			t.Errorf("size %d: the request that broke at 8 should leave the other request's 8 standing", size)
		}
	})
}
