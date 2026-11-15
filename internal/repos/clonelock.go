package repos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// ErrClonesBusy reports that another process holds the clone lock.
var ErrClonesBusy = errors.New("connected-repo clones are being changed by another process")

// clonesLockWait bounds how long connecting a repo waits for a cache pruning
// run, which holds the lock only to re-check and rename one clone.
const clonesLockWait = 30 * time.Second

func (r *Registry) clonesLock() (*flock.Flock, error) {
	if err := os.MkdirAll(r.loc.CacheRoot, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache dir: %w", err)
	}
	return flock.New(filepath.Join(r.loc.CacheRoot, ".clones.lock")), nil
}

// LockClones holds the clone lock while a repo's clone is placed and the
// repo is connected, so a cache pruning run never judges that clone between
// the two steps. The returned function releases it.
func (r *Registry) LockClones(ctx context.Context) (func() error, error) {
	lock, err := r.clonesLock()
	if err != nil {
		return nil, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, clonesLockWait)
	defer cancel()
	if _, err := lock.TryLockContext(waitCtx, 50*time.Millisecond); err != nil {
		return nil, fmt.Errorf("locking connected-repo clones: %w", err)
	}
	return lock.Unlock, nil
}

// ErrCloneInUse reports an sdd-owned clone that must stay; the wrapping error
// says why.
var ErrCloneInUse = errors.New("clone in use")

// CloneHold holds the sdd-owned clone at dir against every other use while it
// is retired, and returns release once it is. It returns ErrCloneInUse,
// wrapped with why, when the clone must stay.
type CloneHold func(dir string) (release func(), err error)

// RetireUnconnectedClone renames the clone out of its place when the
// user-global config, read again under the clone lock, still names no repo
// for it, its disconnection record still stands and, for an sdd-owned clone,
// hold lets it go. It returns where the clone went; "" when the repo is
// connected now or its record is gone. A clone an earlier run retired is
// returned as it is. The caller deletes the returned directory, outside the
// lock. It returns ErrClonesBusy when another process holds the clone lock,
// ErrClonePassRefused on a repos list UnconnectedClones would refuse, and
// hold's error when hold keeps the clone.
func (r *Registry) RetireUnconnectedClone(clone UnconnectedClone, hold CloneHold) (retired string, err error) {
	lock, err := r.clonesLock()
	if err != nil {
		return "", err
	}
	locked, err := lock.TryLock()
	if err != nil {
		return "", fmt.Errorf("locking connected-repo clones: %w", err)
	}
	if !locked {
		return "", ErrClonesBusy
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	path := clone.Dir
	if r.isRetiredClone(clone) {
		return path, nil
	}
	placed, err := r.clonePath(clone)
	if err != nil {
		return "", err
	}
	if placed != path {
		return "", fmt.Errorf("retiring clone %s: %s places its clone at %s", path, clone.RepoID, placed)
	}
	cfg, err := r.Load()
	if err != nil {
		return "", err
	}
	if _, connected := cfg.Connected(clone.RepoID); connected {
		return "", nil
	}
	dropped, err := r.Disconnected(clone.RepoID)
	if err != nil || !dropped {
		return "", err
	}
	err = r.trustRepoList(cfg, 1)
	if err != nil {
		return "", err
	}
	if clone.Owned {
		release, err := hold(path)
		if err != nil {
			return "", err
		}
		defer release()
	}
	err = os.MkdirAll(r.retiredClonesDir(clone.Owned), 0o755)
	if err != nil {
		return "", fmt.Errorf("creating %s: %w", r.retiredClonesDir(clone.Owned), err)
	}
	retired = filepath.Join(r.retiredClonesDir(clone.Owned), entryName(clone.RepoID))
	if err := os.RemoveAll(retired); err != nil {
		return "", fmt.Errorf("removing %s: %w", retired, err)
	}
	if err := os.Rename(path, retired); err != nil {
		return "", fmt.Errorf("retiring clone %s: %w", path, err)
	}
	err = r.clearDisconnectedOnceGone(clone)
	if err != nil {
		return "", err
	}
	return retired, nil
}

// clearDisconnectedOnceGone drops clone's disconnection record unless the
// repo's other clone, sdd-owned or left in the cache, still waits for it.
func (r *Registry) clearDisconnectedOnceGone(clone UnconnectedClone) error {
	other, err := r.clonePath(UnconnectedClone{RepoID: clone.RepoID, Owned: !clone.Owned})
	if err != nil {
		return err
	}
	if IsCloned(other) {
		return nil
	}
	return clearDisconnected(r.loc.DataRoot, clone.RepoID)
}
