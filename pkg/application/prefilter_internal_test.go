package application

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// timedScorer answers one candidate a call at the time answerAt names from
// the scorer's start, and holds a candidate it names no time for until its
// call ends.
type timedScorer struct {
	start    time.Time
	answerAt map[string]time.Duration
}

func (s *timedScorer) Direction() relevance.Direction { return relevance.Both }
func (s *timedScorer) MaxBatch() int                  { return 1 }
func (s *timedScorer) MaxInputTokens() int            { return 0 }
func (s *timedScorer) MaxPairTokens() int             { return 0 }

func (s *timedScorer) Score(ctx context.Context, request relevance.Request) (relevance.Result, error) {
	at, ok := s.answerAt[request.Candidates[0].ID]
	if !ok {
		<-ctx.Done()
		return relevance.Result{}, ctx.Err()
	}
	timer := time.NewTimer(time.Until(s.start.Add(at)))
	defer timer.Stop()
	select {
	case <-timer.C:
		return relevance.Result{Scores: []float64{0.5}}, nil
	case <-ctx.Done():
		return relevance.Result{}, ctx.Err()
	}
}

// judgeLive stops at its time limit once the minimum holds or more than a
// few are missing, keeps judging a few short of it for one further limit,
// waits one further limit for a session without a person, and, for a
// person who chose to wait, runs without a limit until the minimum holds.
func TestJudgeLive_BoundsTheWaitByTheLimitAndTheMinimum(t *testing.T) {
	const limit = 30 * time.Second
	cases := []struct {
		name       string
		budget     judgeBudget
		answers    []time.Duration
		wantScored int
		wantWaited time.Duration
	}{
		{"a few short keep judging until the minimum", judgeBudget{limit: limit, target: 50},
			answersOf(45, 5, 50*time.Second), 50, 50 * time.Second},
		{"a few short stop after one further limit", judgeBudget{limit: limit, target: 50},
			answersOf(45, 5, 90*time.Second), 45, 60 * time.Second},
		{"many short stop at the limit", judgeBudget{limit: limit, target: 50},
			answersOf(30, 10, 45*time.Second), 30, 30 * time.Second},
		{"without a person the check waits one further limit", judgeBudget{limit: limit, target: 50, unattended: true},
			answersOf(30, 10, 45*time.Second), 40, 60 * time.Second},
		{"the minimum held at the limit ends the wait", judgeBudget{limit: limit, target: 50},
			answersOf(50, 5, 45*time.Second), 50, 30 * time.Second},
		{"a person who waits gets the minimum without a limit", judgeBudget{limit: limit, target: 50, wait: true},
			answersOf(45, 5, 200*time.Second), 50, 200 * time.Second},
		{"no limit runs every call to its end", judgeBudget{limit: model.NoJudgeTimeout, target: 50},
			answersOf(45, 15, 100*time.Second), 60, 100 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				scorer := &timedScorer{start: time.Now(), answerAt: map[string]time.Duration{}}
				var pending []relevance.Candidate
				for i := range 60 {
					id := fmt.Sprintf("c%02d", i)
					pending = append(pending, relevance.Candidate{ID: id, Text: id, Line: id})
					if i < len(tc.answers) {
						scorer.answerAt[id] = tc.answers[i]
					}
				}
				scored, waited := judgeLive(t.Context(), Prefilter{Scorer: scorer, Judge: "test:timed"}, "draft", pending, nil, 0, tc.budget)
				if len(scored.fresh) != tc.wantScored || waited != tc.wantWaited {
					t.Fatalf("scored %d after %v, want %d after %v", len(scored.fresh), waited, tc.wantScored, tc.wantWaited)
				}
				if len(scored.broken) > 0 {
					t.Errorf("broken %v: a call stopped by the check's time is no failure of the judge", scored.broken)
				}
			})
		})
	}
}

// answersOf answers the first n candidates at once and the next m at then.
func answersOf(n, m int, then time.Duration) []time.Duration {
	var answers []time.Duration
	for range n {
		answers = append(answers, 0)
	}
	for range m {
		answers = append(answers, then)
	}
	return answers
}
