package application

import (
	"bytes"
	"context"
	"strings"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
)

// GroundingStatus is the rendered calibration of a project's prefilter judge.
type GroundingStatus struct {
	Judge string
	Text  string
}

// PrefilterState is a project's prefilter judge, empty while the prefilter
// is off, whether it holds a recall stage, so its thresholds decide, whether
// a server calibrates it in the background, and the judges whose ranking
// health is weak (model.RankingHealth).
type PrefilterState struct {
	Judge         string
	Calibrated    bool
	AutoCalibrate bool
	Weak          []WeakJudge
}

// WeakJudge is a judge whose ranking recall over Positives referenced labels
// is below model.RankingRecallBar.
type WeakJudge struct {
	Judge     string
	Recall    float64
	Positives int
}

// PrefilterState reports the ranking judge of a reading without a client
// (rankingJudge), its calibration, and the ranking health of every judge.
func (a *Application) PrefilterState(ctx context.Context, identity RequestIdentity, project ProjectID) (PrefilterState, error) {
	_, runtime, err := a.resolve(ctx, identity, project, AccessRead)
	if err != nil {
		return PrefilterState{}, err
	}
	judge := runtime.rankingJudge("").Judge
	ledger, thresholds, err := runtime.judgeCalibration(ctx, judge)
	if err != nil {
		return PrefilterState{}, err
	}
	state := PrefilterState{Judge: judge, Calibrated: thresholds.Valid, AutoCalibrate: runtime.options.Grounding.AutoCalibrate && runtime.options.CalibrationRuns != nil}
	for _, judge := range runtime.judges() {
		if health := model.JudgeRankingHealth(ledger.Labels(judge.Judge)); health.Weak() {
			state.Weak = append(state.Weak, WeakJudge{Judge: judge.Judge, Recall: health.Recall, Positives: health.Positives})
		}
	}
	return state, nil
}

// GroundingStatusRequest selects what GroundingStatus reads: ExcludeReplay
// calibrates on the live labels alone, leaving out those seeded from
// replayed captures; Verbose reports the judge's full calibration.
type GroundingStatusRequest struct {
	ExcludeReplay bool
	Verbose       bool
}

// GroundingStatus reports the labels, the audit and drift state, the
// calibration of the ranking judge of a reading without a client and its
// background calibration (presenters.RenderGroundingSummary). Verbose
// reports that judge's labels, recall bound and stage, ranking recall,
// thresholds, audit and drift state, and the precision per reason code.
func (a *Application) GroundingStatus(ctx context.Context, identity RequestIdentity, project ProjectID, request GroundingStatusRequest) (GroundingStatus, error) {
	_, runtime, err := a.resolve(ctx, identity, project, AccessRead)
	if err != nil {
		return GroundingStatus{}, err
	}
	judge, err := runtime.commandJudge()
	if err != nil {
		return GroundingStatus{}, err
	}
	ledger, err := runtime.relevanceLedger(ctx)
	if err != nil {
		return GroundingStatus{}, err
	}
	auto := presenters.GroundingAuto{On: runtime.options.Grounding.AutoCalibrate && runtime.options.CalibrationRuns != nil}
	if runs := runtime.options.CalibrationRuns; runs != nil {
		run, running, err := runs.Running(ctx)
		if err != nil {
			return GroundingStatus{}, err
		}
		if running {
			shown := model.CalibrationRun(run)
			auto.Run = &shown
		}
	}
	var rendered bytes.Buffer
	if request.Verbose {
		presenters.RenderCalibration(&rendered, ledger.Calibration(judge.Judge, request.ExcludeReplay), auto)
	} else {
		presenters.RenderGroundingSummary(&rendered, ledger.Tally(request.ExcludeReplay), ledger.Calibration(judge.Judge, request.ExcludeReplay), judgeOrigin(runtime, judge), auto)
	}
	return GroundingStatus{Judge: judge.Judge, Text: strings.TrimRight(rendered.String(), "\n")}, nil
}

// judgeOrigin says where judge, the ranking judge of a reading without a
// client, comes from.
func judgeOrigin(runtime *ProjectRuntime, judge Prefilter) string {
	host := runtime.options.Ranking.ClaudeHost
	switch {
	case host.Scorer == nil:
		return "set by `llm_prefilter`"
	case judge.Judge == host.Judge:
		return "default on a Claude host; `llm_prefilter` would replace it"
	}
	return "default on the `llm` provider; `llm_prefilter` would replace it"
}

// RescoreResult reports one re-scoring pass.
type RescoreResult struct {
	Judge string
	// Scored pairs got a score; Missing pairs name an entry the graph no
	// longer holds; Unanswered pairs the judge did not answer.
	Scored, Missing, Unanswered int
	// Requests counts the scoring requests sent, what a reranker bills by.
	Requests int
	// Failed counts the pairs Failures left unscored.
	Failed   int
	Failures []RescoreFailure
}

// RescoreFailure is a subject whose scoring requests failed; its pairs stay
// unscored for the next update.
type RescoreFailure struct {
	Subject string
	Err     error
}

// GroundingUpdateRequest shapes UpdateGrounding: Force scores every
// labelled pair again, its scores replacing the judge's old ones;
// OnProgress, when set, receives the scoring pass's progress, one call at a
// time.
type GroundingUpdateRequest struct {
	Force      bool
	OnProgress func(RescoreProgress)
}

// RescoreProgress is how far a scoring pass has come: Done of the Total
// pairs it scores, counted in pairs so a request split in halves counts
// once. Done counts the pairs scored, failed or too long to score; Failed
// counts the failed ones.
type RescoreProgress struct {
	Judge               string
	Done, Total, Failed int
}

// GroundingUpdate reports one UpdateGrounding: the labels it seeded, then
// its scoring pass.
type GroundingUpdate struct {
	Seeded SeedResult
	Scored RescoreResult
}

// UpdateGrounding appends a replayed reference label for each pair of the
// graph's own references (model.RefPairs) not yet labelled, then has the
// ranking judge of a reading without a client score every labelled pair it
// has not scored, every labelled pair with request.Force, so the judge's
// recall bound and ranking health are computed from the labels at once.
// Each pair is scored from its later entry, the one whose reading judged the
// other, and each request's scores are stored as soon as it returns, so a
// cancelled pass keeps what it paid for. A subject whose requests fail lands
// in Failures and the pass goes on; the error is what stops it: no judge, a
// cancelled ctx, a ledger that cannot be read or written.
func (a *Application) UpdateGrounding(ctx context.Context, identity RequestIdentity, project ProjectID, request GroundingUpdateRequest) (result GroundingUpdate, err error) {
	_, runtime, err := a.resolve(ctx, identity, project, AccessRead)
	if err != nil {
		return GroundingUpdate{}, err
	}
	judge, err := runtime.commandJudge()
	if err != nil {
		return GroundingUpdate{}, err
	}
	store := runtime.options.Relevance
	if store == nil {
		return GroundingUpdate{}, &ApplicationError{Code: ErrorInvalidArgument, Message: "sdd: the project keeps no relevance ledger"}
	}
	save := runtime.appendRelevance
	if request.Force {
		replacer, ok := store.(RelevanceScoreReplacer)
		if !ok {
			return GroundingUpdate{}, &ApplicationError{Code: ErrorInvalidArgument, Message: "sdd: the relevance store cannot replace scores, so a forced update would keep the old ones"}
		}
		save = func(ctx context.Context, batch model.RelevanceBatch) error {
			return replaceRelevance(ctx, replacer, batch)
		}
	}
	selected, err := acquireSnapshotForSearch(ctx, runtime, "", "")
	if err != nil {
		return GroundingUpdate{}, err
	}
	defer selected.releaseInto(&err)
	graph := selected.snapshot.graph
	if result.Seeded, err = seedReplay(ctx, store, model.RefPairs(graph)); err != nil {
		return GroundingUpdate{}, err
	}
	ledger, err := runtime.relevanceLedger(ctx)
	if err != nil {
		return GroundingUpdate{}, err
	}
	pairs := ledger.Unscored(judge.Judge)
	if request.Force {
		pairs = ledger.Scorable()
	}
	result.Scored, err = rescoreJudge(ctx, graph, pairs, judge, request.OnProgress, save)
	if err != nil {
		return GroundingUpdate{}, err
	}
	return result, nil
}
