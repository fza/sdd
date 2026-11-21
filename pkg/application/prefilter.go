package application

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// Prefilter constants: calls in flight per gather and the pass deadline.
const (
	prefilterInFlight = 16
	prefilterDeadline = 45 * time.Second
)

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
}

// runPrefilter rates the candidates not yet scored in the reading once
// they reach verify.prefilter_from. Every candidate a call left
// unanswered (provider down, deadline, unreadable answer) stays to judge
// unscored, and the pass names it. With a floor every call carries its far
// entries, and the pass names the candidates it put below (model.FarFloor).
func runPrefilter(ctx context.Context, prefilter Prefilter, policy GroundingPolicy, subject string, g *model.Graph, candidates []model.GatherCandidate, held map[string]uint8, floor *model.FarFloor) (prefilterPass, error) {
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
	scored := scoreCandidates(ctx, prefilter, subject, pending, farCandidates(g, floor), prefilterDeadline)
	pass.fresh = scored.fresh
	maps.Copy(pass.scores, scored.fresh)
	pass.floored = scored.floored
	for _, candidate := range candidates {
		mark, marked := scored.marks[candidate.Entry.ID]
		if marked && floor.Below(candidate, mark.score, mark.floor) {
			pass.below = append(pass.below, candidate.Entry.ID)
		}
	}
	slices.Sort(pass.below)
	var notes []string
	notes = append(notes, fmt.Sprintf("judge %s scored %d of %d pairs; lines to judge run by falling score", prefilter.Judge, len(pass.fresh), len(pending)))
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

// scoredPairs is what one scoring of pending candidates got: a score byte
// per answered candidate, how many were too long for one call, and how many
// calls left unanswered with their distinct failures; late marks a ranking
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

// scoreCandidates rates pending against subject in filled calls, at most
// prefilterInFlight at once and within deadline. A failed call leaves its
// candidates unscored; the result names it. Every call carries far beside
// its candidates (relevance.ScoreWithFar), unless the scorer's calls have
// no room for them.
func scoreCandidates(ctx context.Context, prefilter Prefilter, subject string, pending, far []relevance.Candidate, deadline time.Duration) scoredPairs {
	scored := scoredPairs{fresh: map[string]uint8{}, marks: map[string]floorMark{}}
	batches, oversize, room := relevance.PlanWithFar(prefilter.Scorer, subject, pending, far)
	if !room {
		far = nil
		batches, oversize = relevance.Plan(prefilter.Scorer, subject, pending)
	}
	scored.floored = len(far) > 0 && len(batches) > 0
	scored.oversize = len(oversize)
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
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
				return nil
			}
			for i, candidate := range request.Candidates {
				scored.fresh[candidate.ID] = model.ScoreByte(result.Scores[i])
				if len(far) > 0 {
					scored.marks[candidate.ID] = floorMark{score: result.Scores[i], floor: result.Floor[i]}
				}
			}
			return nil
		})
	}
	_ = group.Wait()
	return scored
}
