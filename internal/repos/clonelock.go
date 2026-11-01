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

	"github.com/networkteam/sdd/internal/model"
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
// for it, and returns where it went; "" when the repo is connected now. The
// caller deletes the returned directory, outside the lock. It returns
// ErrClonesBusy when another process holds the lock.
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
	cfg, err := r.Load()
	if err != nil {
		return "", err
	}
	if base := filepath.Base(path); strings.HasPrefix(base, ".") && strings.HasSuffix(base, ".pruned") {
		return path, nil
	}
	rel, err := filepath.Rel(r.loc.CacheRoot, path)
	if err != nil {
		return "", err
	}
	if _, connected := cfg.Connected(model.RepoID(filepath.ToSlash(rel))); connected {
		return "", nil
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
