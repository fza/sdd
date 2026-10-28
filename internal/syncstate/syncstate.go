// Package syncstate keeps the state automatic sync shares across processes
// on one graph repository: the sync lock, the graph lock it nests, and the
// blocked-sync record (20261028-192101-d-tac-16h).
package syncstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/gofrs/flock"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
)

// lockRetryDelay is how often a waiting sync lock retries.
const lockRetryDelay = 50 * time.Millisecond

// State is the sync lock and the blocked-sync record of one graph
// repository. Both live in its common git directory, so every worktree and
// every sdd process on the repository shares them.
type State struct {
	dir      string
	graphDir string
}

// Open resolves the repository holding graphDir and its sync state. A graph
// directory outside any repository fails with git.ErrGraphRepoMissing.
func Open(ctx context.Context, graphDir string) (git.CLI, *State, error) {
	root, err := git.GraphRepoRoot(graphDir)
	if err != nil {
		return git.CLI{}, nil, err
	}
	repo := git.CLI{Dir: root}
	common, err := repo.CommonDir(ctx)
	if err != nil {
		return git.CLI{}, nil, fmt.Errorf("locating the sync state of %s: %w", root, err)
	}
	return repo, &State{dir: filepath.Join(common, "sdd"), graphDir: graphDir}, nil
}

// TryLock takes the sync lock when no process holds it, then the graph lock;
// the sync lock is always taken first.
func (s *State) TryLock(context.Context) (func(), error) {
	lock, err := s.syncLock()
	if err != nil {
		return nil, err
	}
	taken, err := lock.TryLock()
	if err != nil || !taken {
		return nil, err
	}
	return s.withGraphLock(lock)
}

// Lock waits for the sync lock until ctx ends, then takes the graph lock.
func (s *State) Lock(ctx context.Context) (func(), error) {
	lock, err := s.syncLock()
	if err != nil {
		return nil, err
	}
	taken, err := lock.TryLockContext(ctx, lockRetryDelay)
	if err != nil {
		return nil, err
	}
	if !taken {
		return nil, errors.New("sync lock not taken")
	}
	return s.withGraphLock(lock)
}

func (s *State) syncLock() (*flock.Flock, error) {
	err := os.MkdirAll(s.dir, 0o755)
	if err != nil {
		return nil, err
	}
	return flock.New(filepath.Join(s.dir, "sync.lock")), nil
}

func (s *State) withGraphLock(syncLock *flock.Flock) (func(), error) {
	graphLock, err := LockGraph(s.graphDir)
	if err != nil {
		Unlock(syncLock)
		return nil, err
	}
	return func() {
		Unlock(graphLock)
		Unlock(syncLock)
	}, nil
}

func (s *State) recordPath() string { return filepath.Join(s.dir, "sync-blocked.json") }

// Load reads the blocked-sync record, nil when sync is not blocked.
func (s *State) Load() (*model.SyncBlocked, error) {
	data, err := os.ReadFile(s.recordPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record model.SyncBlocked
	err = json.Unmarshal(data, &record)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.recordPath(), err)
	}
	return &record, nil
}

// Update replaces the record under its own short-lived lock, so a claim never
// waits for a round's fetch; nil removes it.
func (s *State) Update(change func(*model.SyncBlocked) *model.SyncBlocked) error {
	err := os.MkdirAll(s.dir, 0o755)
	if err != nil {
		return err
	}
	lock := flock.New(filepath.Join(s.dir, "sync-record.lock"))
	err = lock.Lock()
	if err != nil {
		return err
	}
	defer Unlock(lock)
	current, err := s.Load()
	if err != nil {
		return err
	}
	next := change(current)
	if reflect.DeepEqual(current, next) {
		return nil
	}
	if next == nil {
		err = os.Remove(s.recordPath())
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	staged := s.recordPath() + ".tmp"
	err = os.WriteFile(staged, data, 0o644)
	if err != nil {
		return err
	}
	return os.Rename(staged, s.recordPath())
}

// LockGraph serializes every writer of one graph directory: the graph store,
// the Git finalizer and every sync path.
func LockGraph(dir string) (*flock.Flock, error) {
	runtimeDir := filepath.Join(dir, ".sdd-runtime")
	err := os.MkdirAll(runtimeDir, 0o755)
	if err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(runtimeDir, "graph.lock"))
	err = lock.Lock()
	if err != nil {
		return nil, err
	}
	return lock, nil
}

// Unlock releases lock when one was taken.
func Unlock(lock *flock.Flock) {
	if lock != nil {
		_ = lock.Unlock()
	}
}

// HandlerOptions returns opts with the git surfaces and sync state automatic
// sync of graphDir runs on. A graph directory in no repository leaves them
// unset, which a sync round reports as the graph repository missing.
func HandlerOptions(ctx context.Context, graphDir string, opts handlers.Options) (handlers.Options, error) {
	repo, state, err := Open(ctx, graphDir)
	if errors.Is(err, git.ErrGraphRepoMissing) {
		return opts, nil
	}
	if err != nil {
		return opts, err
	}
	opts.GraphDir = graphDir
	opts.GraphFetcher, opts.GraphPuller, opts.Pusher = repo, repo, repo
	opts.SyncLock, opts.SyncRecord = state, state
	return opts, nil
}

// ReadBlocked reads what blocks automatic sync of graphDir's repository, nil
// while sync proceeds; a graph directory in no repository is itself the
// block.
func ReadBlocked(ctx context.Context, graphDir string) (*model.SyncBlocked, error) {
	_, state, err := Open(ctx, graphDir)
	if errors.Is(err, git.ErrGraphRepoMissing) {
		return &model.SyncBlocked{Kind: model.SyncBlockedGraphMissing, Since: time.Now(), Detail: graphDir}, nil
	}
	if err != nil {
		return nil, err
	}
	return state.Load()
}
