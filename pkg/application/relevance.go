package application

import (
	"cmp"
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

// prefilterCalibration is judgeCalibration of gather's prefilter judge.
func (r *ProjectRuntime) prefilterCalibration(ctx context.Context) (*model.RelevanceLedger, model.RelevanceThresholds, error) {
	return r.judgeCalibration(ctx, r.options.Prefilter.Judge)
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

// The aliases a RescoreRequest names a judge by: gather's prefilter judge,
// and the deep reading's ranking judge under a Claude host.
const (
	JudgeAliasPrefilter = "prefilter"
	JudgeAliasRanking   = "ranking"
)

// judges are the runtime's distinct prefilter judges: gather's and the
// Claude host's ranking judge.
func (r *ProjectRuntime) judges() []Prefilter {
	var judges []Prefilter
	for _, judge := range []Prefilter{r.options.Prefilter, r.options.Ranking.ClaudeHost} {
		if judge.Scorer != nil && !slices.ContainsFunc(judges, func(held Prefilter) bool { return held.Judge == judge.Judge }) {
			judges = append(judges, judge)
		}
	}
	return judges
}

// scoringJudges are the judges a rescore of name scores with: the one name
// or its alias selects, or for an empty name every judge, the listwise one
// first, so a slow per-pair judge never holds back the batched one.
func (r *ProjectRuntime) scoringJudges(name string) ([]Prefilter, error) {
	judges := r.judges()
	if len(judges) == 0 {
		return nil, &ApplicationError{Code: ErrorInvalidArgument, Message: "sdd: no prefilter judge: verify.prefilter is off"}
	}
	if name == "" {
		order := func(judge Prefilter) int {
			if strings.HasPrefix(judge.Judge, string(model.PrefilterListwise)+":") {
				return 0
			}
			return 1
		}
		slices.SortStableFunc(judges, func(a, b Prefilter) int { return cmp.Compare(order(a), order(b)) })
		return judges, nil
	}
	aliases := map[string]Prefilter{JudgeAliasPrefilter: r.options.Prefilter, JudgeAliasRanking: r.options.Ranking.ClaudeHost}
	if judge, ok := aliases[name]; ok && judge.Scorer != nil {
		return []Prefilter{judge}, nil
	}
	var available []string
	for _, judge := range judges {
		if judge.Judge == name {
			return []Prefilter{judge}, nil
		}
		var named []string
		for _, alias := range []string{JudgeAliasPrefilter, JudgeAliasRanking} {
			if aliases[alias].Scorer != nil && aliases[alias].Judge == judge.Judge {
				named = append(named, alias)
			}
		}
		available = append(available, fmt.Sprintf("%s (%s)", judge.Judge, strings.Join(named, ", ")))
	}
	return nil, &ApplicationError{Code: ErrorInvalidArgument, Message: fmt.Sprintf("sdd: unknown judge %q: pass one of %s", name, strings.Join(available, ", "))}
}
