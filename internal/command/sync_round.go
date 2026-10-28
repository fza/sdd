package command

import (
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// FetchGraphCmd captures intent to fetch the graph repository's upstream and
// stamp the last-fetch marker in SDDDir.
type FetchGraphCmd struct {
	SDDDir string
	// Cooldown skips the fetch when the last one is more recent; zero always
	// fetches.
	Cooldown time.Duration
	// OnFetched is invoked with the outcome: SyncStateFetched, or the state
	// that kept the fetch from running or succeeding, with its reason.
	OnFetched func(state model.SyncState, reason string)
}

// SyncRoundCmd captures intent to run one automatic sync round of the graph
// repository: fetch, merge upstream graph commits, publish local ones, and
// record what blocks them (20261028-201909-d-tac-fxp).
type SyncRoundCmd struct {
	SDDDir   string
	Cooldown time.Duration
	// Explicit marks a round the user asked for: it waits for the sync lock
	// and runs while a conflict pauses automatic sync.
	Explicit bool
	// OnSkipped is invoked when another process holds the sync lock and the
	// round did not run.
	OnSkipped func()
}

// ReleaseSyncOwnerCmd captures intent to give up a session's ownership of the
// blocked sync, when that session ends.
type ReleaseSyncOwnerCmd struct {
	Session string
	// OnNotice is invoked with the banner while sync is blocked.
	OnNotice func(notice string)
}

// ClaimSyncNoticeCmd captures intent to deliver the blocked-sync notice to
// one agent session, making it the owner of the fix when nobody is.
type ClaimSyncNoticeCmd struct {
	Session string
	// OnNotice is invoked with the notice while sync is blocked.
	OnNotice func(notice string)
}
