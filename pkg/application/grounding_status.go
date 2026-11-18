package application

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// GroundingStatus is the rendered calibration of a project's prefilter judge.
type GroundingStatus struct {
	Judge string
	Text  string
}

// PrefilterState is a project's prefilter judge, empty while the prefilter
// is off, whether its recall bound holds, so its thresholds decide, and the
// judges whose ranking health is weak (model.RankingHealth).
type PrefilterState struct {
	Judge      string
	Calibrated bool
	Weak       []WeakJudge
}

// WeakJudge is a judge whose ranking recall over Positives referenced labels
// is below model.RankingRecallBar.
type WeakJudge struct {
	Judge     string
	Recall    float64
	Positives int
}

// PrefilterState reports the project's prefilter judge, its calibration and
// the ranking health of every judge.
func (a *Application) PrefilterState(ctx context.Context, identity RequestIdentity, project ProjectID) (PrefilterState, error) {
	_, runtime, err := a.resolve(ctx, identity, project, AccessRead)
	if err != nil {
		return PrefilterState{}, err
	}
	ledger, thresholds, err := runtime.prefilterCalibration(ctx)
	if err != nil {
		return PrefilterState{}, err
	}
	state := PrefilterState{Judge: runtime.options.Prefilter.Judge, Calibrated: thresholds.Valid}
	for _, judge := range runtime.judges() {
		if health := model.JudgeRankingHealth(ledger.Labels(judge.Judge)); health.Weak() {
			state.Weak = append(state.Weak, WeakJudge{Judge: judge.Judge, Recall: health.Recall, Positives: health.Positives})
		}
	}
	return state, nil
}

// GroundingStatusRequest selects what GroundingStatus reads: ExcludeReplay
// calibrates on the live labels alone, leaving out those seeded from
// replayed captures.
type GroundingStatusRequest struct {
	ExcludeReplay bool
}

// GroundingStatus reports each judge's labels, recall bound, ranking recall,
// thresholds, audit and drift state, and the precision per reason code:
// gather's prefilter judge, then the Claude host's ranking judge where it
// differs.
func (a *Application) GroundingStatus(ctx context.Context, identity RequestIdentity, project ProjectID, request GroundingStatusRequest) (GroundingStatus, error) {
	_, runtime, err := a.resolve(ctx, identity, project, AccessRead)
	if err != nil {
		return GroundingStatus{}, err
	}
	judges := runtime.judges()
	if len(judges) == 0 {
		return GroundingStatus{}, &ApplicationError{Code: ErrorInvalidArgument, Message: "sdd: no prefilter judge: verify.prefilter is off"}
	}
	ledger, err := runtime.relevanceLedger(ctx)
	if err != nil {
		return GroundingStatus{}, err
	}
	var rendered bytes.Buffer
	for i, judge := range judges {
		if i > 0 {
			rendered.WriteString("\n")
		}
		if judge.Judge != runtime.options.Prefilter.Judge {
			rendered.WriteString("the deep reading's ranking judge under a Claude host:\n")
		}
		presenters.RenderCalibration(&rendered, ledger.Calibration(judge.Judge, request.ExcludeReplay))
	}
	return GroundingStatus{Judge: judges[0].Judge, Text: strings.TrimRight(rendered.String(), "\n")}, nil
}

// RescoreResult reports one re-scoring pass.
type RescoreResult struct {
	Judge string
	// Scored pairs got a score; Missing pairs name an entry the graph no
	// longer holds; Unanswered pairs the judge did not answer.
	Scored, Missing, Unanswered int
	// Requests counts the scoring requests sent, what a reranker bills by.
	Requests int
}

// RescoreRelevance has each judge (gather's prefilter judge, then the Claude
// host's ranking judge where it differs) score every labelled pair it has
// not scored, so a new judge's recall bound and ranking health are computed
// from the stored labels at once. Each pair is scored from its later entry,
// the one whose reading judged the other.
func (a *Application) RescoreRelevance(ctx context.Context, identity RequestIdentity, project ProjectID) (results []RescoreResult, err error) {
	_, runtime, err := a.resolve(ctx, identity, project, AccessRead)
	if err != nil {
		return nil, err
	}
	judges := runtime.judges()
	if len(judges) == 0 {
		return nil, &ApplicationError{Code: ErrorInvalidArgument, Message: "sdd: no prefilter judge: verify.prefilter is off"}
	}
	ledger, err := runtime.relevanceLedger(ctx)
	if err != nil {
		return nil, err
	}
	selected, err := acquireSnapshotForSearch(ctx, runtime, "", "")
	if err != nil {
		return nil, err
	}
	defer selected.releaseInto(&err)
	for _, judge := range judges {
		result, err := rescoreJudge(ctx, runtime, ledger, selected.snapshot.graph, judge)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// rescoreJudge has prefilter score the labelled pairs of ledger it has not
// scored and stores the scores.
func rescoreJudge(ctx context.Context, runtime *ProjectRuntime, ledger *model.RelevanceLedger, graph *model.Graph, prefilter Prefilter) (RescoreResult, error) {
	result := RescoreResult{Judge: prefilter.Judge}
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
			candidates = append(candidates, relevance.Candidate{ID: id, Text: model.EntryRelevanceText(graph, graph.ByID[id]), Line: model.EntryRankingLine(graph.ByID[id])})
		}
		batches, oversize := relevance.Plan(prefilter.Scorer, model.EntryRelevanceText(graph, subject), candidates)
		result.Unanswered += len(oversize)
		result.Requests += len(batches)
		requests := make([]relevance.Request, len(batches))
		answers := make([]relevance.Result, len(batches))
		group, groupCtx := errgroup.WithContext(ctx)
		group.SetLimit(prefilterInFlight)
		for b, indexes := range batches {
			requests[b].Subject = model.EntryRelevanceText(graph, subject)
			for _, index := range indexes {
				requests[b].Candidates = append(requests[b].Candidates, candidates[index])
			}
			group.Go(func() error {
				scored, err := prefilter.Scorer.Score(groupCtx, requests[b])
				if err == nil {
					err = relevance.CheckResult(requests[b], scored)
				}
				answers[b] = scored
				return err
			})
		}
		err := group.Wait()
		if err != nil {
			return RescoreResult{}, fmt.Errorf("sdd: rescoring the labelled pairs of %s: %w", subjectID, err)
		}
		for b, request := range requests {
			scored := answers[b]
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
