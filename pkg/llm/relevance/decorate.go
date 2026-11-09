package relevance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"github.com/networkteam/sdd/pkg/llm"
)

// decorated forwards a Scorer's limits and replaces its Score.
type decorated struct {
	Scorer
	score func(context.Context, Request) (Result, error)
}

func (d decorated) Score(ctx context.Context, request Request) (Result, error) {
	return d.score(ctx, request)
}

// Bounded gives every call a deadline.
func Bounded(s Scorer, timeout time.Duration) Scorer {
	return decorated{Scorer: s, score: func(ctx context.Context, request Request) (Result, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return s.Score(ctx, request)
	}}
}

// RateLimited holds calls to rps per second with a burst of one second's
// worth, at least one.
func RateLimited(s Scorer, rps float64) Scorer {
	limiter := rate.NewLimiter(rate.Limit(rps), max(int(rps), 1))
	return decorated{Scorer: s, score: func(ctx context.Context, request Request) (Result, error) {
		if err := limiter.Wait(ctx); err != nil {
			return Result{}, fmt.Errorf("rate limiter: %w", err)
		}
		return s.Score(ctx, request)
	}}
}

// Observed hands one llm.CallStat per call to sink, as llm.Observed does;
// Items is the number of candidates. A nil sink returns s unwrapped.
func Observed(s Scorer, sink llm.StatsSink) Scorer {
	if sink == nil {
		return s
	}
	return decorated{Scorer: s, score: func(ctx context.Context, request Request) (Result, error) {
		start := time.Now()
		result, err := s.Score(ctx, request)
		stat := llm.CallStat{Purpose: Purpose, Items: len(request.Candidates), Duration: time.Since(start)}
		if err != nil {
			if attributed, ok := errors.AsType[*llm.Error](err); ok {
				stat.Identity = attributed.Identity
				stat.Usage = attributed.Usage
			}
			stat.Error = err.Error()
		} else {
			stat.Identity = result.Identity
			stat.Usage = result.Usage
		}
		sink.RecordCall(ctx, stat)
		return result, err
	}}
}

// PerItem turns a one-pair scorer into a batch scorer: each candidate of a
// batch goes in its own call, at most concurrency at once. The batch fails
// on the first failed call; its usage sums every call's.
func PerItem(s Scorer, concurrency int) Scorer {
	return perItem{inner: s, concurrency: max(concurrency, 1)}
}

type perItem struct {
	inner       Scorer
	concurrency int
}

func (p perItem) Direction() Direction { return p.inner.Direction() }
func (p perItem) MaxBatch() int        { return 0 }
func (p perItem) MaxInputTokens() int  { return p.inner.MaxInputTokens() }

func (p perItem) Score(ctx context.Context, request Request) (Result, error) {
	results := make([]Result, len(request.Candidates))
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(p.concurrency)
	for i, candidate := range request.Candidates {
		group.Go(func() error {
			one := Request{Subject: request.Subject, Candidates: []Candidate{candidate}}
			result, err := p.inner.Score(ctx, one)
			if err != nil {
				return err
			}
			if err := CheckResult(one, result); err != nil {
				return err
			}
			results[i] = result
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return Result{}, err
	}
	out := Result{Scores: make([]float64, len(results))}
	for i, result := range results {
		out.Scores[i] = result.Scores[0]
		out.Identity = result.Identity
		out.Usage.InputTokens += result.Usage.InputTokens
		out.Usage.OutputTokens += result.Usage.OutputTokens
		out.Usage.CacheReadTokens += result.Usage.CacheReadTokens
		out.Usage.CacheCreateTokens += result.Usage.CacheCreateTokens
		out.Usage.ReasoningTokens += result.Usage.ReasoningTokens
		out.Usage.CostUSD += result.Usage.CostUSD
	}
	return out, nil
}
