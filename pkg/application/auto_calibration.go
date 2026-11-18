package application

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/model"
)

// Background calibration constants: the candidates one subject's request
// holds, the subjects scored at once, and the pause after a round that left
// nothing to score and after one whose requests failed.
const (
	autoCalibrateBatch    = 60
	autoCalibrateInFlight = 4
	autoCalibrateRecheck  = 5 * time.Minute
	autoCalibrateRetry    = 30 * time.Minute
)

// CalibrationRun is a background calibration run as its process records it:
// the judge it scores with, when it started, the stage the judge held at the
// last record, the pairs it scored, and whether it holds its next request
// until the server is idle.
type CalibrationRun struct {
	Judge   string
	Started time.Time
	Stage   float64
	Scored  int
	Waiting bool
}

// CalibrationRuns admits one background calibration of a project at a time,
// across processes, and shares the state its holder records.
type CalibrationRuns interface {
	// Acquire admits a run; ok is false while another holds one.
	Acquire(ctx context.Context) (lease CalibrationLease, ok bool, err error)
	// Running returns the state the holder of a run recorded; ok is false
	// while no run is held.
	Running(ctx context.Context) (run CalibrationRun, ok bool, err error)
}

// CalibrationLease is a run Acquire admitted, held until Release.
type CalibrationLease interface {
	Record(ctx context.Context, run CalibrationRun) error
	Release() error
}

// AutoCalibrationRequest shapes AutoCalibrate: WaitIdle blocks until the
// server is idle or ctx ends; nil never waits.
type AutoCalibrationRequest struct {
	WaitIdle func(ctx context.Context) error
}

// AutoCalibrate keeps the ranking judge of a reading without a client
// scoring its unscored labelled pairs in the background until ctx ends,
// while verify.auto_calibrate and the prefilter are on and the judge holds
// no top recall stage. On a ledger without a labelled pair it first labels
// the graph's own references. Each subject holding an unscored positive
// label goes in one request with a sample of its other labelled candidates
// (model.RelevanceLedger.CalibrationBatches), and each request's scores are
// stored as soon as it returns, so a server that quits loses no paid call
// and the next one resumes with the pairs still unscored. Scoring toward the
// next stage starts at once; once a stage is reached, every further request
// waits for request.WaitIdle. One process runs at a time
// (ProjectRuntimeOptions.CalibrationRuns). Starts, stages and failures go to
// the log.
func (a *Application) AutoCalibrate(ctx context.Context, identity RequestIdentity, project ProjectID, request AutoCalibrationRequest) {
	calibrator := &autoCalibrator{app: a, identity: identity, project: project, request: request, free: true, skipped: map[string]bool{}}
	for {
		wait := calibrator.round(ctx)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

type autoCalibrator struct {
	app      *Application
	identity RequestIdentity
	project  ProjectID
	request  AutoCalibrationRequest
	// free holds until the first stage this server reached; skipped are the
	// subjects whose requests failed or held only candidates too long to
	// score, left until the next server.
	free    bool
	skipped map[string]bool
}

// round runs one calibration pass and returns the pause before the next.
func (c *autoCalibrator) round(ctx context.Context) time.Duration {
	log := slogutils.FromContext(ctx)
	_, runtime, err := c.app.resolve(ctx, c.identity, c.project, AccessRead)
	if err != nil {
		if ctx.Err() == nil {
			log.Warn("auto-calibration: resolving the project failed", "err", err)
		}
		return autoCalibrateRetry
	}
	policy, runs := runtime.options.Grounding, runtime.options.CalibrationRuns
	if !policy.AutoCalibrate || !policy.Prefilter || runs == nil || runtime.options.Relevance == nil {
		return autoCalibrateRecheck
	}
	judge, err := runtime.commandJudge()
	if err != nil {
		return autoCalibrateRecheck
	}
	ledger, err := runtime.relevanceLedger(ctx)
	if err != nil {
		log.Warn("auto-calibration: reading the relevance ledger failed", "err", err)
		return autoCalibrateRetry
	}
	stage := judgeStage(ledger, judge.Judge)
	if stage == model.RecallFloor || len(ledger.Scorable()) > 0 && len(c.pending(ledger, judge.Judge)) == 0 {
		return autoCalibrateRecheck
	}
	lease, held, err := runs.Acquire(ctx)
	if err != nil {
		log.Warn("auto-calibration: acquiring the run failed", "err", err)
		return autoCalibrateRetry
	}
	if !held {
		return autoCalibrateRecheck
	}
	defer func() {
		if err := lease.Release(); err != nil {
			log.Warn("auto-calibration: releasing the run failed", "err", err)
		}
	}()
	subjects, err := c.plan(ctx, runtime, judge)
	if err != nil {
		if ctx.Err() == nil {
			log.Warn("auto-calibration: planning the requests failed", "judge", judge.Judge, "err", err)
		}
		return autoCalibrateRetry
	}
	if len(subjects) == 0 {
		return autoCalibrateRecheck
	}
	return c.score(ctx, runtime, judge, lease, subjects, stage)
}

// pending are the batches ledger plans for judge, past the skipped
// subjects.
func (c *autoCalibrator) pending(ledger *model.RelevanceLedger, judge string) []model.CalibrationBatch {
	return slices.DeleteFunc(ledger.CalibrationBatches(judge, autoCalibrateBatch), func(batch model.CalibrationBatch) bool { return c.skipped[batch.Subject] })
}

// plan labels the graph's own references on a ledger holding no labelled
// pair, then reads the texts of the pending batches from one snapshot.
func (c *autoCalibrator) plan(ctx context.Context, runtime *ProjectRuntime, judge Prefilter) (subjects []scoringSubject, err error) {
	selected, err := acquireSnapshotForSearch(ctx, runtime, "", "")
	if err != nil {
		return nil, err
	}
	defer selected.releaseInto(&err)
	graph := selected.snapshot.graph
	ledger, err := runtime.relevanceLedger(ctx)
	if err != nil {
		return nil, err
	}
	if len(ledger.Scorable()) == 0 {
		seeded, err := seedReplay(ctx, runtime.options.Relevance, model.RefPairs(graph))
		if err != nil {
			return nil, err
		}
		if seeded.Added == 0 {
			return nil, nil
		}
		slogutils.FromContext(ctx).Info("auto-calibration: labelled the graph's own references", "pairs", seeded.Added)
		if ledger, err = runtime.relevanceLedger(ctx); err != nil {
			return nil, err
		}
	}
	for _, batch := range c.pending(ledger, judge.Judge) {
		if graph.ByID[batch.Subject] == nil {
			continue
		}
		candidates := slices.DeleteFunc(slices.Clone(batch.Candidates), func(id string) bool { return graph.ByID[id] == nil })
		subjects = append(subjects, newScoringSubject(graph, batch.Subject, candidates))
	}
	return subjects, nil
}

// score sends subjects, autoCalibrateInFlight at once, recording the run
// through lease. It stops dispatching at the top stage, waits for an idle
// server before each subject once a stage was reached, and returns the
// pause before the next round.
func (c *autoCalibrator) score(ctx context.Context, runtime *ProjectRuntime, judge Prefilter, lease CalibrationLease, subjects []scoringSubject, stage float64) time.Duration {
	log := slogutils.FromContext(ctx)
	var mu sync.Mutex
	run := CalibrationRun{Judge: judge.Judge, Started: c.app.now(), Stage: stage}
	record := func() {
		if err := lease.Record(context.WithoutCancel(ctx), run); err != nil {
			log.Warn("auto-calibration: recording the run failed", "err", err)
		}
	}
	log.Info("auto-calibration started: the ranking judge scores its unscored labelled pairs, which spends model calls", "judge", judge.Judge, "stage", stage, "subjects", len(subjects))
	record()
	var (
		wg       sync.WaitGroup
		top      bool
		failed   int
		fatal    error
		inFlight = make(chan struct{}, autoCalibrateInFlight)
	)
	onRequest := func(done int, failedRequest bool) {
		if failedRequest {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		run.Scored += done
		ledger, err := runtime.relevanceLedger(context.WithoutCancel(ctx))
		if err != nil {
			log.Warn("auto-calibration: reading the relevance ledger failed", "err", err)
			return
		}
		held := judgeStage(ledger, judge.Judge)
		if held > run.Stage {
			log.Info("auto-calibration reached a recall stage", "judge", judge.Judge, "stage", held, "pairs scored", run.Scored)
			run.Stage, c.free = held, false
			top = held == model.RecallFloor
			record()
		}
	}
dispatch:
	for _, subject := range subjects {
		mu.Lock()
		halt, free := top || fatal != nil, c.free
		mu.Unlock()
		if halt || ctx.Err() != nil {
			break
		}
		if !free && c.request.WaitIdle != nil {
			mu.Lock()
			run.Waiting = true
			record()
			mu.Unlock()
			err := c.request.WaitIdle(ctx)
			mu.Lock()
			run.Waiting = false
			record()
			mu.Unlock()
			if err != nil {
				break
			}
		}
		select {
		case inFlight <- struct{}{}:
		case <-ctx.Done():
			break dispatch
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-inFlight }()
			outcome, err := scoreSubject(ctx, judge, subject, runtime.appendRelevance, onRequest)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if ctx.Err() == nil {
					fatal = err
				}
				return
			}
			if outcome.Failure != nil || outcome.Scored == 0 {
				c.skipped[subject.ID] = true
			}
			if outcome.Failure != nil {
				failed++
				log.Warn("auto-calibration: scoring failed, the subject's pairs stay unscored until the next server", "judge", judge.Judge, "subject", subject.ID, "err", outcome.Failure)
			}
			record()
		}()
	}
	wg.Wait()
	switch {
	case ctx.Err() != nil:
		log.Info("auto-calibration stopped with the server; the next one resumes with the pairs still unscored", "judge", judge.Judge, "stage", run.Stage, "pairs scored", run.Scored)
		return 0
	case fatal != nil:
		log.Warn("auto-calibration: storing scores failed", "judge", judge.Judge, "err", fatal)
		return autoCalibrateRetry
	}
	log.Info("auto-calibration finished its round", "judge", judge.Judge, "stage", run.Stage, "pairs scored", run.Scored, "failed subjects", failed)
	if failed > 0 {
		return autoCalibrateRetry
	}
	return autoCalibrateRecheck
}

// judgeStage is the recall stage judge's labels hold in ledger.
func judgeStage(ledger *model.RelevanceLedger, judge string) float64 {
	return model.Calibrate(ledger.Labels(judge), ledger.Steps(), judge).Stage
}
