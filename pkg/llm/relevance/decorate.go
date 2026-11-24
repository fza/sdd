package relevance

import (
	"context"
	"errors"
	"fmt"
	"time"

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

func (d decorated) Reads(candidate Candidate) bool { return Reads(d.Scorer, candidate) }

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
