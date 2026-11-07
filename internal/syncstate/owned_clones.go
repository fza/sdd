package syncstate

import (
	"context"
	"path/filepath"
	"time"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/repos"
)

// OwnedClones answers what cache pruning asks of an sdd-owned clone
// (handlers.OwnedCloneProbe): its sync lock, the commits it holds unpushed,
// and when the git gate last let an operation reach its remote.
type OwnedClones struct {
	Gate *gitgate.Gate
}

// TryLockSync takes the sync lock of the clone at dir when no process holds
// it; release is nil otherwise.
func (o OwnedClones) TryLockSync(ctx context.Context, dir string) (func(), error) {
	common, err := git.CLI{Dir: dir}.CommonDir(ctx)
	if err != nil {
		return nil, err
	}
	state := &State{dir: filepath.Join(common, "sdd")}
	lock, err := state.syncLock()
	if err != nil {
		return nil, err
	}
	taken, err := lock.TryLock()
	if err != nil || !taken {
		return nil, err
	}
	return func() { Unlock(lock) }, nil
}

func (o OwnedClones) Unpushed(ctx context.Context, dir string) (bool, error) {
	return git.CLI{Dir: dir}.HoldsUnpushed(ctx)
}

func (o OwnedClones) LastAttempt(ctx context.Context, dir string) (time.Time, error) {
	if o.Gate == nil {
		return time.Time{}, repos.ErrNoGate
	}
	url, err := git.CLI{Dir: dir}.UpstreamURL(ctx)
	if err != nil {
		return time.Time{}, err
	}
	target, err := gitgate.TargetFromURL(url)
	if err != nil {
		return time.Time{}, err
	}
	return o.Gate.LastAttempt(ctx, target)
}
