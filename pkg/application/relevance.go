package application

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
)

// RelevanceStore keeps the relevance verdict ledger as immutable segments in
// the ledger's binary format. Append stores a copy of the segment; Segments
// returns stored segments in any order. The application folds them into one
// ledger, so the order of writes and duplicates do not matter, and a store may
// merge segments it holds into one, as the local stores' compaction does, as
// long as the merge folds to the same ledger.
type RelevanceStore interface {
	Append(ctx context.Context, segment []byte) error
	Segments(ctx context.Context) ([][]byte, error)
}

// relevanceLedger loads the project's ledger, an empty one when the project
// has no store.
func (r *ProjectRuntime) relevanceLedger(ctx context.Context) (*model.RelevanceLedger, error) {
	if r.options.Relevance == nil {
		return model.NewRelevanceLedger(), nil
	}
	segments, err := r.options.Relevance.Segments(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading the relevance ledger: %w", err)
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		return nil, fmt.Errorf("loading the relevance ledger: %w", err)
	}
	return ledger, nil
}

// appendRelevance stores batch, nothing when the project has no store.
func (r *ProjectRuntime) appendRelevance(ctx context.Context, batch model.RelevanceBatch) error {
	if r.options.Relevance == nil || batch.Empty() {
		return nil
	}
	err := r.options.Relevance.Append(ctx, relevance.Encode(batch))
	if err != nil {
		return fmt.Errorf("storing relevance verdicts: %w", err)
	}
	return nil
}

// judgeCalibration loads the ledger and judge's calibrated thresholds:
// invalid without a judge or while the recall bound does not hold.
func (r *ProjectRuntime) judgeCalibration(ctx context.Context, judge string) (*model.RelevanceLedger, model.RelevanceThresholds, error) {
	ledger, err := r.relevanceLedger(ctx)
	if err != nil {
		return nil, model.RelevanceThresholds{}, err
	}
	if judge == "" {
		return ledger, model.RelevanceThresholds{}, nil
	}
	return ledger, model.Calibrate(ledger.Labels(judge), ledger.Steps(), judge), nil
}

// judges are the runtime's distinct ranking judges, the one a reading
// without a client ranks with first (rankingJudge), then the Claude host's
// and the prefilter's.
func (r *ProjectRuntime) judges() []Prefilter {
	var judges []Prefilter
	for _, judge := range []Prefilter{r.rankingJudge(""), r.options.Ranking.ClaudeHost, r.options.Prefilter} {
		if judge.Scorer != nil && !slices.ContainsFunc(judges, func(held Prefilter) bool { return held.Judge == judge.Judge }) {
			judges = append(judges, judge)
		}
	}
	return judges
}

// scoringJudge is the judge a rescore of name scores with: for an empty name
// the one a reading without a client ranks with, else the judge so named, a
// legacy name (model.CanonicalJudge) included.
func (r *ProjectRuntime) scoringJudge(name string) (Prefilter, error) {
	judges := r.judges()
	if len(judges) == 0 {
		return Prefilter{}, &ApplicationError{Code: ErrorInvalidArgument, Message: "sdd: no prefilter judge: verify.prefilter is off"}
	}
	if name == "" {
		return judges[0], nil
	}
	var available []string
	for _, judge := range judges {
		if judge.Judge == model.CanonicalJudge(name) {
			return judge, nil
		}
		available = append(available, judge.Judge)
	}
	return Prefilter{}, &ApplicationError{Code: ErrorInvalidArgument, Message: fmt.Sprintf("sdd: unknown judge %q: pass one of %s", name, strings.Join(available, ", "))}
}
