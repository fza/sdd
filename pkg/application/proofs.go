package application

import (
	"context"
	"fmt"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
)

// ProofStore keeps the proof records of the hiding rules (model.ProofRecords)
// as immutable segments, machine-local like the relevance ledger: Append
// stores a copy of the segment; Segments returns stored segments in any
// order, folded with duplicates dropped.
type ProofStore interface {
	Append(ctx context.Context, segment []byte) error
	Segments(ctx context.Context) ([][]byte, error)
}

// proofRecords loads the project's proof records, none when the project
// keeps no store.
func (r *ProjectRuntime) proofRecords(ctx context.Context) (model.ProofRecords, error) {
	if r.options.Proofs == nil {
		return model.ProofRecords{}, nil
	}
	segments, err := r.options.Proofs.Segments(ctx)
	if err != nil {
		return model.ProofRecords{}, fmt.Errorf("loading the proof records: %w", err)
	}
	records, err := relevance.FoldProofs(segments)
	if err != nil {
		return model.ProofRecords{}, fmt.Errorf("loading the proof records: %w", err)
	}
	return records, nil
}

// appendProofs stores records, nothing when the project keeps no store.
func (r *ProjectRuntime) appendProofs(ctx context.Context, records model.ProofRecords) error {
	if r.options.Proofs == nil || len(records.Replays)+len(records.Tallies)+len(records.Scans)+len(records.Misses) == 0 {
		return nil
	}
	segment, err := relevance.EncodeProofs(records)
	if err != nil {
		return err
	}
	if err := r.options.Proofs.Append(ctx, segment); err != nil {
		return fmt.Errorf("storing proof records: %w", err)
	}
	return nil
}

// hidingStates are the standing of both hiding rules for judge on a graph
// of graphSize live entries: thresholds calibrated on the live labels and
// the judge's replayed checks (model.Calibrate over model.ReplayLabels),
// and each rule's proof and step.
type hidingStates struct {
	thresholds model.RelevanceThresholds
	threshold  model.RuleState
	floor      model.RuleState
}

// judgeHiding loads the ledger and the proof records and folds judge's
// hiding states.
func (r *ProjectRuntime) judgeHiding(ctx context.Context, judge Prefilter, graphSize int) (*model.RelevanceLedger, model.ProofRecords, hidingStates, error) {
	ledger, err := r.relevanceLedger(ctx)
	if err != nil {
		return nil, model.ProofRecords{}, hidingStates{}, err
	}
	records, err := r.proofRecords(ctx)
	if err != nil {
		return nil, model.ProofRecords{}, hidingStates{}, err
	}
	if judge.Judge == "" {
		return ledger, records, hidingStates{}, nil
	}
	states := hidingStates{thresholds: model.Calibrate(model.CheckedLabels(ledger.Labels(judge.Judge), records.Replays, judge.Judge, judge.Revision), ledger.Steps(), judge.Judge)}
	states.threshold = model.HidingState(model.RuleThreshold, records, judge.Judge, judge.Revision, graphSize, states.thresholds)
	states.floor = model.HidingState(model.RuleFarFloor, records, judge.Judge, judge.Revision, graphSize, states.thresholds)
	return ledger, records, states, nil
}
