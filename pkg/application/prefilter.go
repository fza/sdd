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

// Prefilter constants (20261109-004506-d-cpt-udz, 5.8): calls in flight per
// gather and the pass deadline.
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
}

// runPrefilter rates the candidates not yet scored in the reading once
// they reach verify.prefilter_from (D27). Every candidate a call left
// unanswered (provider down, deadline, unreadable answer) stays to judge
// unscored, and the pass names it (5.8).
func runPrefilter(ctx context.Context, runtime *ProjectRuntime, policy GroundingPolicy, subject string, g *model.Graph, candidates []model.GatherCandidate, held map[string]uint8) (prefilterPass, error) {
	pass := prefilterPass{status: model.PrefilterOff, scores: map[string]uint8{}, fresh: map[string]uint8{}}
	for id, score := range held {
		pass.scores[id] = score
	}
	prefilter := runtime.options.Prefilter
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
			pending = append(pending, relevance.Candidate{ID: candidate.Entry.ID, Text: model.EntryRelevanceText(g, candidate.Entry)})
		}
	}
	pass.status = model.PrefilterOK
	scored := scoreCandidates(ctx, prefilter, subject, pending)
	pass.fresh = scored.fresh
	maps.Copy(pass.scores, scored.fresh)
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
// calls left unanswered with their distinct failures.
type scoredPairs struct {
	fresh      map[string]uint8
	oversize   int
	unanswered int
	failures   []string
}

// scoreCandidates rates pending against subject in filled calls, at most
// prefilterInFlight at once and within prefilterDeadline (5.8, D30). A
// failed call leaves its candidates unscored; the result names it.
func scoreCandidates(ctx context.Context, prefilter Prefilter, subject string, pending []relevance.Candidate) scoredPairs {
	scored := scoredPairs{fresh: map[string]uint8{}}
	batches, oversize := relevance.Plan(prefilter.Scorer, subject, pending)
	scored.oversize = len(oversize)
	ctx, cancel := context.WithTimeout(ctx, prefilterDeadline)
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
			result, err := prefilter.Scorer.Score(ctx, request)
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
			}
			return nil
		})
	}
	_ = group.Wait()
	return scored
}
