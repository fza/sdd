package model

import "time"

// RepoSyncState is what a sync round did for one connected repository
// (20261107-095427-d-tac-yux).
type RepoSyncState string

const (
	// RepoSynced: fetched and merged, or cloned, with nothing to push.
	RepoSynced RepoSyncState = "synced"
	// RepoPushed: the round pushed commits that were waiting.
	RepoPushed RepoSyncState = "pushed"
	// RepoSkipped: the git gate deferred the round's operations until Next.
	RepoSkipped RepoSyncState = "skipped"
	// RepoFailed: an operation failed for Reason; Next is when the gate
	// admits the fetch again.
	RepoFailed RepoSyncState = "failed"
	// RepoRefused: the repository is not connected on this machine.
	RepoRefused RepoSyncState = "refused"
)

// RepoSyncOutcome is the outcome of one connected repository's sync round.
type RepoSyncOutcome struct {
	RepoID RepoID
	State  RepoSyncState
	Next   time.Time
	Reason string
}
