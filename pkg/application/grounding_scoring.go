package application

import (
	"context"
	"sync"

	"github.com/networkteam/slogutils"
	"golang.org/x/sync/errgroup"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// rescoreJudge has prefilter score pairs, subject by subject, storing each
// request's scores through save as soon as it returns and reporting its
// progress to onProgress.
func rescoreJudge(ctx context.Context, graph *model.Graph, pairs []model.RelevancePair, prefilter Prefilter, onProgress func(RescoreProgress), save func(context.Context, model.RelevanceBatch) error) (RescoreResult, error) {
	result := RescoreResult{Judge: prefilter.Judge}
	progress := RescoreProgress{Judge: prefilter.Judge}
	var progressMu sync.Mutex
	advance := func(done int, failed bool) {
		if onProgress == nil {
			return
		}
		progressMu.Lock()
		defer progressMu.Unlock()
		progress.Done += done
		if failed {
			progress.Failed += done
		}
		onProgress(progress)
	}
	bySubject := map[string][]string{}
	var subjects []string
	for _, pair := range pairs {
		if graph.ByID[pair.A] == nil || graph.ByID[pair.B] == nil {
			result.Missing++
			continue
		}
		if _, ok := bySubject[pair.B]; !ok {
			subjects = append(subjects, pair.B)
		}
		bySubject[pair.B] = append(bySubject[pair.B], pair.A)
		progress.Total++
	}
	advance(0, false)
	for _, subjectID := range subjects {
		outcome, err := scoreSubject(ctx, prefilter, newScoringSubject(graph, subjectID, bySubject[subjectID]), save, advance)
		if left := outcome.Oversize + outcome.Unread; left > 0 {
			advance(left, false)
		}
		result.Requests += outcome.Requests
		result.Unanswered += outcome.Oversize
		result.Unread += outcome.Unread
		result.Scored += outcome.Scored
		result.Failed += outcome.Failed
		if err != nil {
			return RescoreResult{}, err
		}
		if outcome.Failure != nil {
			slogutils.FromContext(ctx).Info("scoring failed, going on with the next subject", "judge", prefilter.Judge, "subject", subjectID, "err", outcome.Failure)
			result.Failures = append(result.Failures, RescoreFailure{Subject: subjectID, Err: outcome.Failure})
		}
	}
	return result, nil
}

// scoringSubject is one subject's candidates a scoring pass sends, their
// texts read from the graph.
type scoringSubject struct {
	ID         string
	Text       string
	Candidates []relevance.Candidate
}

func newScoringSubject(graph *model.Graph, subjectID string, candidateIDs []string) scoringSubject {
	subject := scoringSubject{ID: subjectID, Text: model.EntryRelevanceText(graph, graph.ByID[subjectID])}
	for _, id := range candidateIDs {
		subject.Candidates = append(subject.Candidates, relevance.Candidate{ID: id, Text: model.EntryRelevanceText(graph, graph.ByID[id]), Line: model.EntryRankingLine(graph.ByID[id])})
	}
	return subject
}

// subjectOutcome is what scoring one subject did: its requests, its
// candidates too long for one, those the judge cannot read, those scored
// and those its failed requests left, with the first failure.
type subjectOutcome struct {
	Requests, Oversize, Unread, Scored, Failed int
	Failure                                    error
}

// scoreSubject has prefilter score subject's candidates in the requests
// relevance.Plan splits them into, at most prefilterInFlight at once. Each
// request's scores are stored through save as soon as it returns, even when
// ctx ends meanwhile, then reported to onRequest with the candidates it held
// and whether it failed. The error is what stops the pass: a cancelled ctx
// or a failed save.
func scoreSubject(ctx context.Context, prefilter Prefilter, subject scoringSubject, save func(context.Context, model.RelevanceBatch) error, onRequest func(done int, failed bool)) (subjectOutcome, error) {
	batches, oversize, unread := relevance.Plan(prefilter.Scorer, subject.Text, subject.Candidates)
	outcome := subjectOutcome{Requests: len(batches), Oversize: len(oversize), Unread: len(unread)}
	forward := prefilter.Scorer.Direction() == relevance.Forward
	var mu sync.Mutex
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(prefilterInFlight)
	for _, indexes := range batches {
		request := relevance.Request{Subject: subject.Text}
		for _, index := range indexes {
			request.Candidates = append(request.Candidates, subject.Candidates[index])
		}
		group.Go(func() error {
			scored, err := prefilter.Scorer.Score(groupCtx, request)
			if err == nil {
				err = relevance.CheckResult(request, scored)
			}
			if err != nil {
				mu.Lock()
				outcome.Failed += len(request.Candidates)
				if outcome.Failure == nil {
					outcome.Failure = err
				}
				mu.Unlock()
				onRequest(len(request.Candidates), true)
				return nil
			}
			var batch model.RelevanceBatch
			for i, candidate := range request.Candidates {
				pair := model.NewRelevancePair(subject.ID, candidate.ID)
				direction := model.DirectionBoth
				if forward {
					direction = model.DirectionFrom(pair, subject.ID)
				}
				batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: prefilter.Judge, Direction: direction, Score: model.ScoreByte(scored.Scores[i])}})
			}
			if err := save(context.WithoutCancel(ctx), batch); err != nil {
				return err
			}
			mu.Lock()
			outcome.Scored += len(request.Candidates)
			mu.Unlock()
			onRequest(len(request.Candidates), false)
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return outcome, err
	}
	return outcome, ctx.Err()
}
