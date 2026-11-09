package application

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// GroundingStatus is the rendered calibration of a project's prefilter
// judge (20261109-004506-d-cpt-udz, 7.3).
type GroundingStatus struct {
	Judge string
	Text  string
}

// GroundingStatus reports the current judge's labels, recall bound,
// thresholds, audit and drift state, and the precision per reason code.
func (a *Application) GroundingStatus(ctx context.Context, identity RequestIdentity, project ProjectID) (GroundingStatus, error) {
	_, runtime, err := a.resolve(ctx, identity, project, AccessRead)
	if err != nil {
		return GroundingStatus{}, err
	}
	judge := runtime.options.Prefilter.Judge
	if judge == "" {
		return GroundingStatus{}, &ApplicationError{Code: ErrorInvalidArgument, Message: "sdd: no prefilter judge: set verify.prefilter and configure llm_prefilter"}
	}
	ledger, err := runtime.relevanceLedger(ctx)
	if err != nil {
		return GroundingStatus{}, err
	}
	var rendered bytes.Buffer
	presenters.RenderCalibration(&rendered, ledger.Calibration(judge))
	return GroundingStatus{Judge: judge, Text: strings.TrimRight(rendered.String(), "\n")}, nil
}

// RescoreResult reports one re-scoring pass.
type RescoreResult struct {
	Judge string
	// Scored pairs got a score; Missing pairs name an entry the graph no
	// longer holds; Unanswered pairs the judge did not answer.
	Scored, Missing, Unanswered int
}

// RescoreRelevance has the current judge score every labelled pair it has
// not scored, so a new judge's recall bound is computed from the stored
// labels at once (D36). Each pair is scored from its later entry, the one
// whose reading judged the other.
func (a *Application) RescoreRelevance(ctx context.Context, identity RequestIdentity, project ProjectID) (result RescoreResult, err error) {
	_, runtime, err := a.resolve(ctx, identity, project, AccessRead)
	if err != nil {
		return RescoreResult{}, err
	}
	prefilter := runtime.options.Prefilter
	if prefilter.Scorer == nil {
		return RescoreResult{}, &ApplicationError{Code: ErrorInvalidArgument, Message: "sdd: no prefilter judge: set verify.prefilter and configure llm_prefilter"}
	}
	result.Judge = prefilter.Judge
	ledger, err := runtime.relevanceLedger(ctx)
	if err != nil {
		return RescoreResult{}, err
	}
	selected, err := acquireSnapshotForSearch(ctx, runtime, "", "")
	if err != nil {
		return RescoreResult{}, err
	}
	defer selected.releaseInto(&err)
	graph := selected.snapshot.graph
	bySubject := map[string][]string{}
	var subjects []string
	for _, pair := range ledger.Unscored(prefilter.Judge) {
		if graph.ByID[pair.A] == nil || graph.ByID[pair.B] == nil {
			result.Missing++
			continue
		}
		if _, ok := bySubject[pair.B]; !ok {
			subjects = append(subjects, pair.B)
		}
		bySubject[pair.B] = append(bySubject[pair.B], pair.A)
	}
	forward := prefilter.Scorer.Direction() == relevance.Forward
	var batch model.RelevanceBatch
	for _, subjectID := range subjects {
		subject := graph.ByID[subjectID]
		var candidates []relevance.Candidate
		for _, id := range bySubject[subjectID] {
			candidates = append(candidates, relevance.Candidate{ID: id, Text: model.EntryRelevanceText(graph, graph.ByID[id])})
		}
		batches, oversize := relevance.Plan(prefilter.Scorer, model.EntryRelevanceText(graph, subject), candidates)
		result.Unanswered += len(oversize)
		for _, indexes := range batches {
			request := relevance.Request{Subject: model.EntryRelevanceText(graph, subject)}
			for _, index := range indexes {
				request.Candidates = append(request.Candidates, candidates[index])
			}
			scored, err := prefilter.Scorer.Score(ctx, request)
			if err == nil {
				err = relevance.CheckResult(request, scored)
			}
			if err != nil {
				return RescoreResult{}, fmt.Errorf("sdd: rescoring the labelled pairs of %s: %w", subjectID, err)
			}
			for i, candidate := range request.Candidates {
				pair := model.NewRelevancePair(subjectID, candidate.ID)
				direction := model.DirectionBoth
				if forward {
					direction = model.DirectionFrom(pair, subjectID)
				}
				batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: prefilter.Judge, Direction: direction, Score: model.ScoreByte(scored.Scores[i])}})
			}
		}
	}
	result.Scored = len(batch.Scores)
	if err := runtime.appendRelevance(ctx, batch); err != nil {
		return RescoreResult{}, err
	}
	return result, nil
}
