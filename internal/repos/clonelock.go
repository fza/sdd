package repos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
// for it and, for an sdd-owned clone, hold lets it go. It returns where the
// clone went; "" when the repo is connected now. The caller deletes the
// returned directory, outside the lock. It returns ErrClonesBusy when another
// process holds the clone lock, and hold's error when hold keeps the clone.
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
	cfg, err := r.Load()
	if err != nil {
		return "", err
	}
	path := clone.Dir
	if base := filepath.Base(path); strings.HasPrefix(base, ".") && strings.HasSuffix(base, ".pruned") {
		return path, nil
	}
	if _, connected := cfg.Connected(clone.RepoID); connected {
		return "", nil
	}
	if clone.Owned {
		release, err := hold(path)
		if err != nil {
			return "", err
		}
		defer release()
	}
	retired = filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".pruned")
	if err := os.RemoveAll(retired); err != nil {
		return "", fmt.Errorf("removing %s: %w", retired, err)
	}
	if err := os.Rename(path, retired); err != nil {
		return "", fmt.Errorf("retiring clone %s: %w", path, err)
	}
	return retired, nil
}
