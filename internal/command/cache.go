package command

import "github.com/networkteam/sdd/internal/model"

// PruneCacheCmd deletes the cache data past its retention: replaced and
// unused index versions, unused index stores, graph snapshots and embedder
// calibrations, clones sdd repo remove dropped that no connection names, and
// crash leftovers. One run per machine at a time: a second one asked for by
// hand fails at once, a scheduled one returns at once.
type PruneCacheCmd struct {
	// Scheduled marks a background run, which also returns at once when
	// another run started less than model.CachePruneInterval ago.
	Scheduled bool

	// OnPruned reports what the run removed, also when it failed partway.
	OnPruned func(model.CachePruneCounts)
}
