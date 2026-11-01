package command

import "github.com/networkteam/sdd/internal/model"

// PruneCacheCmd deletes the cache data background pruning removes
// (20261101-131514-d-cpt-9ci): replaced and unused index versions, unused
// index stores, graph snapshots of vanished folders, and clones no connection
// names. One run per machine at a time; a second returns at once.
type PruneCacheCmd struct {
	// Scheduled marks a background run, which returns at once when another run
	// started less than model.CachePruneInterval ago.
	Scheduled bool

	// OnPruned reports what the run removed, also when it failed partway.
	OnPruned func(model.CachePruneCounts)
}
