package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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
	// team is the committed record of the judge, nil without one.
	team *model.TeamProof
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
	if states.team, err = r.teamProof(ctx, judge.Judge); err != nil {
		return nil, model.ProofRecords{}, hidingStates{}, err
	}
	// A machine whose own replays prove nothing yet starts at the team's step.
	if !states.threshold.Proof.Valid() && states.team != nil && states.team.Applies(judge.Judge, judge.Revision, graphSize) && states.team.Thresholds.Valid {
		states.thresholds = states.team.TeamThresholds()
	}
	states.threshold = model.ApplyTeamProof(states.threshold, states.team, records, judge.Judge, judge.Revision, graphSize)
	states.floor = model.ApplyTeamProof(states.floor, states.team, records, judge.Judge, judge.Revision, graphSize)
	return ledger, records, states, nil
}

// TeamProofs reads and commits the team's proof records (model.TeamProof)
// in the graph's repository, one per judge key (model.TeamProofKey), so a
// machine that newly joins a graph starts at the team's step.
type TeamProofs interface {
	ReadTeamProof(ctx context.Context, key string) (data []byte, found bool, err error)
	WriteTeamProof(ctx context.Context, key string, data []byte) error
}

// teamProof reads judge's committed record, nil without one or without a
// store.
func (r *ProjectRuntime) teamProof(ctx context.Context, judge string) (*model.TeamProof, error) {
	if r.options.TeamProofs == nil {
		return nil, nil
	}
	key := model.TeamProofKey(judge)
	data, found, err := r.options.TeamProofs.ReadTeamProof(ctx, key)
	if err != nil || !found {
		return nil, err
	}
	var team model.TeamProof
	if err := json.Unmarshal(data, &team); err != nil {
		return nil, fmt.Errorf("reading the team proof %s: %w", key, err)
	}
	return &team, nil
}

// recordTeamProof commits the machine's standing for judge as the team's
// record when it differs from the committed one in what a newcomer would
// start at (model.TeamProofDue). A standing taken from the record keeps
// the record's graph size.
func (r *ProjectRuntime) recordTeamProof(ctx context.Context, judge Prefilter, graphSize int, states hidingStates, now time.Time) error {
	if r.options.TeamProofs == nil {
		return nil
	}
	base := graphSize
	if states.team != nil && (states.threshold.Team || states.floor.Team) {
		base = states.team.GraphSize
	}
	current := model.NewTeamProof(judge.Judge, judge.Revision, base, states.thresholds, []model.RuleState{states.threshold, states.floor}, now)
	if !model.TeamProofDue(states.team, current, graphSize) {
		return nil
	}
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	return r.options.TeamProofs.WriteTeamProof(ctx, model.TeamProofKey(judge.Judge), append(data, '\n'))
}
