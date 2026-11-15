package repos

import (
	"context"
	"errors"
	"fmt"
	"net/url"
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

// LockClones holds the clone lock while a repo's clone is moved into the
// cache and the repo is connected, so a cache pruning run never judges that
// clone between the two steps. The returned function releases it.
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

// RetireUnconnectedClone renames the clone at path out of the cache when the
// user-global config, read again under the clone lock, still names no repo
// for it and its disconnection record still stands, and returns where it went;
// "" when the repo is connected again. A clone an earlier run retired is
// returned as it is. The caller deletes the returned directory, outside the
// lock. It returns ErrClonesBusy when another process holds the lock, and
// ErrClonePassRefused on a repos list UnconnectedClones would refuse.
func (r *Registry) RetireUnconnectedClone(path string) (retired string, err error) {
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
	if filepath.Dir(path) == r.retiredClonesDir() {
		return path, nil
	}
	cfg, err := r.Load()
	if err != nil {
		return "", err
	}
	repoID, err := r.cloneRepoID(path)
	if err != nil {
		return "", err
	}
	if _, connected := cfg.Connected(repoID); connected {
		return "", nil
	}
	dropped, err := r.Disconnected(repoID)
	if err != nil || !dropped {
		return "", err
	}
	err = r.trustRepoList(cfg, 1)
	if err != nil {
		return "", err
	}
	err = os.MkdirAll(r.retiredClonesDir(), 0o755)
	if err != nil {
		return "", fmt.Errorf("creating %s: %w", r.retiredClonesDir(), err)
	}
	retired = filepath.Join(r.retiredClonesDir(), url.PathEscape(string(repoID)))
	if err := os.RemoveAll(retired); err != nil {
		return "", fmt.Errorf("removing %s: %w", retired, err)
	}
	if err := os.Rename(path, retired); err != nil {
		return "", fmt.Errorf("retiring clone %s: %w", path, err)
	}
	if err := clearDisconnected(r.loc.CacheRoot, repoID); err != nil {
		return "", err
	}
	return retired, nil
}
