package command

import (
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// FetchGraphCmd captures intent to fetch the graph repository's upstream
// when the git gate admits it.
type FetchGraphCmd struct {
	// OnFetched is invoked with the outcome: SyncStateFetched, or the state
	// that kept the fetch from running or succeeding, with its reason; a fetch
	// the gate deferred is SyncStateSkipped.
	OnFetched func(state model.SyncState, reason string)
	// OnDeferred is invoked with the time a fetch the gate deferred may run.
	OnDeferred func(operation string, next time.Time)
}

// SyncRoundCmd captures intent to run one automatic sync round of the graph
// repository: fetch, merge upstream graph commits, publish local ones, and
// record what blocks them (20261028-201909-d-tac-fxp).
type SyncRoundCmd struct {
	// Explicit marks a round the user asked for: it waits for the sync lock
	// and runs while a conflict pauses automatic sync.
	Explicit bool
	// OnSkipped is invoked when another process holds the sync lock and the
	// round did not run.
	OnSkipped func()
	// OnDeferred is invoked per operation the git gate deferred, with the
	// time it may run.
	OnDeferred func(operation string, next time.Time)
	// OnPushed is invoked after a push with the number of commits it
	// published.
	OnPushed func(commits int)
}

// ReleaseSyncOwnerCmd captures intent to give up a session's ownership of the
// blocked sync, when that session ends.
type ReleaseSyncOwnerCmd struct {
	Session string
	// TitlesOnly releases only a failed title backfill, under manual sync.
	TitlesOnly bool
	// OnNotice is invoked with the banner while sync is blocked.
	OnNotice func(notice string)
}

// ClaimSyncNoticeCmd captures intent to deliver the blocked-sync notice to
// one agent session, making it the owner of the fix when nobody is.
type ClaimSyncNoticeCmd struct {
	Session string
	// TitlesOnly serves only a failed title backfill, under manual sync.
	TitlesOnly bool
	// OnNotice is invoked with the notice while sync is blocked.
	OnNotice func(notice string)
}
