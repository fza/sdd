package application

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// prefilterInFlight is how many calls a gather holds in flight.
const prefilterInFlight = 16

// Prefilter is the composed relevance scorer of a project and the judge
// calibration keys its labels on; a nil Scorer leaves the prefilter off.
type Prefilter struct {
	Scorer relevance.Scorer
	Judge  string
}

// prefilterPass is what one gather's prefilter did.
type prefilterPass struct {
	status string
	note   string
	// fresh are the scores this pass added; scores every score the
	// placement uses, those the reading already held included.
	fresh  map[string]uint8
	scores map[string]uint8
	// below are the candidates of fresh the far floor put below it, sorted;
	// floored reports that the pass's calls carried the far entries.
	below   []string
	floored bool
	// judged reports that the judge ran: then scored counts the candidates
	// holding a score, waited how long the check waited, and broken the
	// failures of its calls other than running out of time.
	judged bool
	scored int
	waited time.Duration
	broken []string
}

// judgeBudget bounds a live check's judge: limit, model.NoJudgeTimeout for
// none, the count of candidates it waits for (model.JudgeTarget), wait for
// a person who chose to wait until that count, and unattended for a session
// without a person.
type judgeBudget struct {
	limit      time.Duration
	target     int
	wait       bool
	unattended bool
}

// runPrefilter rates the candidates not yet scored in the reading once
// they reach verify.prefilter_from, within budget (judgeLive). Every
// candidate a call left unanswered (provider down, time limit, unreadable
// answer) stays to judge unscored, and the pass names it. With a floor
// every call carries its far entries, and the pass names the candidates it
// put below (model.FarFloor).
func runPrefilter(ctx context.Context, prefilter Prefilter, policy GroundingPolicy, budget judgeBudget, subject string, g *model.Graph, candidates []model.GatherCandidate, held map[string]uint8, floor *model.FarFloor) (prefilterPass, error) {
	pass := prefilterPass{status: model.PrefilterOff, scores: map[string]uint8{}, fresh: map[string]uint8{}}
	for id, score := range held {
		pass.scores[id] = score
	}
	switch {
	case !policy.Prefilter:
		return pass, nil
	case prefilter.Scorer == nil:
		pass.note = "verify.prefilter is on, but no llm_prefilter is configured"
		return pass, nil
	case strings.TrimSpace(subject) == "":
		pass.note = "no draft to rate the candidates against"
		return pass, nil
	case len(candidates) < policy.PrefilterFrom:
		pass.note = fmt.Sprintf("%d pairs to judge, below verify.prefilter_from (%d): judge the briefs", len(candidates), policy.PrefilterFrom)
		return pass, nil
	}
	if err := secrets.CheckText("the draft", subject); err != nil {
		return prefilterPass{}, err
	}
	var pending []relevance.Candidate
	for _, candidate := range candidates {
		if _, ok := held[candidate.Entry.ID]; !ok {
			pending = append(pending, relevance.Candidate{ID: candidate.Entry.ID, Text: model.EntryRelevanceText(g, candidate.Entry), Line: model.EntryRankingLine(candidate.Entry)})
		}
	}
	pass.status = model.PrefilterOK
	scored, waited := judgeLive(ctx, prefilter, subject, pending, farCandidates(g, floor), len(candidates)-len(pending), budget)
	pass.fresh = scored.fresh
	maps.Copy(pass.scores, scored.fresh)
	pass.floored = scored.floored
	pass.judged, pass.waited, pass.broken = true, waited, scored.broken
	for _, candidate := range candidates {
		if _, ok := pass.scores[candidate.Entry.ID]; ok {
			pass.scored++
		}
		mark, marked := scored.marks[candidate.Entry.ID]
		if marked && floor.Below(candidate, mark.score, mark.floor) {
			pass.below = append(pass.below, candidate.Entry.ID)
		}
	}
	slices.Sort(pass.below)
	tally := fmt.Sprintf("judge %s scored %d of %d pairs", prefilter.Judge, len(pass.fresh), len(pending))
	// Without a score, fresh or held by the reading, no line has a rank to run by.
	if pass.scored > 0 {
		tally += "; lines to judge run by falling score"
	}
	notes := []string{tally}
	if scored.oversize > 0 {
		notes = append(notes, fmt.Sprintf("%d too long for one call, left to judge uncut", scored.oversize))
	}
	if scored.unanswered > 0 {
		notes = append(notes, fmt.Sprintf("%d unanswered and left to judge (%s)", scored.unanswered, strings.Join(scored.failures, "; ")))
		pass.status = model.PrefilterPartial
		if len(pass.fresh) == 0 {
			pass.status = model.PrefilterFailed
		}
	}
	pass.note = strings.Join(notes, ", ")
	return pass, nil
}

// judgeLive scores pending as scoreCandidates does, held candidates of the
// list already scored, within budget. Without a limit the judge runs to the
// end, or, for a person who chose to wait, until budget.target candidates
// hold a score. With a limit it stops there once the target holds; short of
// it the check keeps judging until the target holds or one further limit
// passes, where at most model.JudgeKeepsJudging candidates are missing or
// the session has no person to choose. It returns how long it waited.
func judgeLive(ctx context.Context, prefilter Prefilter, subject string, pending, far []relevance.Candidate, held int, budget judgeBudget) (scoredPairs, time.Duration) {
	start := time.Now()
	var run context.Context
	var cancel context.CancelFunc
	timed := !budget.wait && budget.limit > 0
	if timed {
		run, cancel = context.WithTimeout(ctx, 2*budget.limit)
	} else {
		run, cancel = context.WithCancel(ctx)
	}
	defer cancel()
	var judged atomic.Int64
	judged.Store(int64(held))
	reached := make(chan struct{})
	var once sync.Once
	onScored := func(n int) {
		if judged.Add(int64(n)) >= int64(budget.target) {
			once.Do(func() { close(reached) })
		}
	}
	onScored(0)
	done := make(chan scoredPairs, 1)
	go func() { done <- scoreCandidatesUntil(run, prefilter, subject, pending, far, onScored) }()
	stop := func() (scoredPairs, time.Duration) {
		cancel()
		scored := <-done
		return scored, time.Since(start)
	}
	if !timed {
		if !budget.wait {
			return <-done, time.Since(start)
		}
		select {
		case scored := <-done:
			return scored, time.Since(start)
		case <-reached:
			return stop()
		}
	}
	timer := time.NewTimer(budget.limit)
	defer timer.Stop()
	select {
	case scored := <-done:
		return scored, time.Since(start)
	case <-timer.C:
	}
	missing := int64(budget.target) - judged.Load()
	if missing <= 0 || !budget.unattended && missing > model.JudgeKeepsJudging {
		return stop()
	}
	select {
	case scored := <-done:
		return scored, time.Since(start)
	case <-reached:
		return stop()
	}
}

// scoredPairs is what one scoring of pending candidates got: a score byte
// per answered candidate, how many were too long for one call, and how many
// calls left unanswered with their distinct failures, broken those other
// than running out of time or being stopped; late marks a ranking
// collected before every pass ended. floored reports that every call
// carried the far entries; marks then hold each answered candidate's score
// beside the floor its own call measured.
type scoredPairs struct {
	fresh      map[string]uint8
	marks      map[string]floorMark
	floored    bool
	oversize   int
	unanswered int
	failures   []string
	broken     []string
	late       bool
}

// floorMark is a candidate's score and the median score its call gave the
// far entries.
type floorMark struct {
	score, floor float64
}

// farCandidates are the far entries of floor as a judge call carries them,
// in the form of every other candidate; none without a floor.
func farCandidates(g *model.Graph, floor *model.FarFloor) []relevance.Candidate {
	if floor == nil {
		return nil
	}
	far := make([]relevance.Candidate, 0, len(floor.Far))
	for _, e := range floor.Far {
		far = append(far, relevance.Candidate{ID: e.ID, Text: model.EntryRelevanceText(g, e), Line: model.EntryRankingLine(e)})
	}
	return far
}

// scoreCandidates rates pending against subject within deadline
// (scoreCandidatesUntil).
func scoreCandidates(ctx context.Context, prefilter Prefilter, subject string, pending, far []relevance.Candidate, deadline time.Duration) scoredPairs {
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	return scoreCandidatesUntil(ctx, prefilter, subject, pending, far, nil)
}

// scoreCandidatesUntil rates pending against subject in filled calls, at
// most prefilterInFlight at once, until ctx ends, and hands onScored the
// count of each call's answered candidates. A failed call leaves its
// candidates unscored; the result names it. Every call carries far beside
// its candidates (relevance.ScoreWithFar), unless the scorer's calls have
// no room for them.
func scoreCandidatesUntil(ctx context.Context, prefilter Prefilter, subject string, pending, far []relevance.Candidate, onScored func(n int)) scoredPairs {
	scored := scoredPairs{fresh: map[string]uint8{}, marks: map[string]floorMark{}}
	batches, oversize, room := relevance.PlanWithFar(prefilter.Scorer, subject, pending, far)
	if !room {
		far = nil
		batches, oversize = relevance.Plan(prefilter.Scorer, subject, pending)
	}
	scored.floored = len(far) > 0 && len(batches) > 0
	scored.oversize = len(oversize)
	var mu sync.Mutex
	group := errgroup.Group{}
	group.SetLimit(prefilterInFlight)
	for _, batch := range batches {
		group.Go(func() error {
			request := relevance.Request{Subject: subject}
			for _, index := range batch {
				request.Candidates = append(request.Candidates, pending[index])
			}
			result, err := relevance.ScoreWithFar(ctx, prefilter.Scorer, request, far)
			if err == nil {
				err = relevance.CheckResult(request, result)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				scored.unanswered += len(request.Candidates)
				if !slices.Contains(scored.failures, err.Error()) {
					scored.failures = append(scored.failures, err.Error())
				}
				stopped := errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
				if !stopped && !slices.Contains(scored.broken, err.Error()) {
					scored.broken = append(scored.broken, err.Error())
				}
				return nil
			}
			for i, candidate := range request.Candidates {
				scored.fresh[candidate.ID] = model.ScoreByte(result.Scores[i])
				if len(far) > 0 {
					scored.marks[candidate.ID] = floorMark{score: result.Scores[i], floor: result.Floor[i]}
				}
			}
			if onScored != nil {
				onScored(len(request.Candidates))
			}
			return nil
		})
	}
	_ = group.Wait()
	return scored
}
