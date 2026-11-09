package application

import (
	"context"
	"fmt"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
)

// RelevanceStore keeps the relevance verdict ledger (20261109-004506-d-cpt-udz,
// 5.7) as immutable segments in the ledger's binary format, one per append.
// Append stores a copy of the segment and rewrites nothing stored; Segments
// returns every stored segment in any order. The application folds them
// into one ledger, so the order of writes and duplicates do not matter.
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
