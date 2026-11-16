package command

import "time"

// SyncPullCmd captures intent to pull the shared graph from the upstream
// branch using a merge (never a rebase), so background sync never rewrites
// the shared history. The handler refuses on a dirty working tree and defers
// to the user. On a successful pull it invokes OnPulled with git's own output
// (the "Already up to date." line or the merge summary).
type SyncPullCmd struct {
	// OnPulled is invoked after a successful merge pull with git's combined
	// stdout/stderr — the text the user wants to see verbatim.
	OnPulled func(output string)
	// OnDeferred is invoked instead when the git gate deferred the pull, with
	// the time it may run.
	OnDeferred func(next time.Time)
}
