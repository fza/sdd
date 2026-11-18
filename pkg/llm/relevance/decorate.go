package relevance

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
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

// RetryDelayer is an error asking for a delay before the next call, as a
// 429's Retry-After does.
type RetryDelayer interface {
	error
	RetryDelay() time.Duration
}

// MaxRetryAfter caps the delay a Retry-After asks for.
const MaxRetryAfter = time.Hour

// ParseRetryAfter is the delay a Retry-After value asks for at now, in
// seconds or as an HTTP date, at most MaxRetryAfter; zero for none.
func ParseRetryAfter(value string, now time.Time) time.Duration {
	seconds, err := strconv.Atoi(value)
	if err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(min(seconds, int(MaxRetryAfter/time.Second))) * time.Second
	}
	at, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	return min(max(at.Sub(now), 0), MaxRetryAfter)
}

// RetriedAfterDelay calls once more after a failure whose RetryDelayer asks
// for a delay. The wait adds up to a quarter of the delay at random, so calls
// refused together come back apart; a wait past maxWait or ctx's deadline
// ends the call, as does any other failure.
func RetriedAfterDelay(s Scorer, maxWait time.Duration) Scorer {
	return decorated{Scorer: s, score: func(ctx context.Context, request Request) (Result, error) {
		result, err := s.Score(ctx, request)
		delayer, ok := errors.AsType[RetryDelayer](err)
		if !ok {
			return result, err
		}
		delay := delayer.RetryDelay()
		if delay <= 0 {
			return result, err
		}
		wait := delay + rand.N(delay/4+1)
		deadline, bounded := ctx.Deadline()
		if wait > maxWait || bounded && time.Until(deadline) <= wait {
			return result, err
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return result, err
		case <-timer.C:
		}
		return s.Score(ctx, request)
	}}
}

// RetriedOnce calls once more after any failure while ctx lives: a listwise
// call that breaks its answer seldom breaks it twice.
func RetriedOnce(s Scorer) Scorer {
	return decorated{Scorer: s, score: func(ctx context.Context, request Request) (Result, error) {
		result, err := s.Score(ctx, request)
		if err == nil || ctx.Err() != nil {
			return result, err
		}
		return s.Score(ctx, request)
	}}
}

// Limited holds at most n calls in flight across every request, at least
// one.
func Limited(s Scorer, n int) Scorer {
	slots := make(chan struct{}, max(n, 1))
	return decorated{Scorer: s, score: func(ctx context.Context, request Request) (Result, error) {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
		defer func() { <-slots }()
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

// PerItem fans a one-pair scorer out: each candidate goes in its own call, at
// most concurrency at once across every request. It keeps the inner
// MaxBatch, so Plan gives each candidate its own call and the decorators
// around PerItem act per call. A request of several candidates fails on the
// first failed call; its usage sums every call's.
func PerItem(s Scorer, concurrency int) Scorer {
	concurrency = max(concurrency, 1)
	return perItem{inner: s, concurrency: concurrency, slots: make(chan struct{}, concurrency)}
}

type perItem struct {
	inner       Scorer
	concurrency int
	slots       chan struct{}
}

func (p perItem) Direction() Direction { return p.inner.Direction() }
func (p perItem) MaxBatch() int        { return p.inner.MaxBatch() }
func (p perItem) MaxInputTokens() int  { return p.inner.MaxInputTokens() }
func (p perItem) MaxPairTokens() int   { return p.inner.MaxPairTokens() }

func (p perItem) Score(ctx context.Context, request Request) (Result, error) {
	results := make([]Result, len(request.Candidates))
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(p.concurrency)
	for i, candidate := range request.Candidates {
		group.Go(func() error {
			select {
			case p.slots <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}
			defer func() { <-p.slots }()
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
