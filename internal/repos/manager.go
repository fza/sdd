package repos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/gitgate"
)

// ErrNoGate reports a git network operation asked of a component built
// without the gate: a wiring gap, never a user state.
var ErrNoGate = errors.New("git network operation without the git gate")

// Manager owns the side-effectful half of the connected-repos machinery: the
// sdd-owned clone of each connected repository, made on first use. It wraps
// the pure Registry and is the dependency handlers get injected — the
// write-side counterpart to the finder-injected Registry.
type Manager struct {
	reg    *Registry
	clones *GraphClones
}

// NewManager builds a Manager over the registry and the clone owner.
func NewManager(reg *Registry, clones *GraphClones) *Manager {
	return &Manager{reg: reg, clones: clones}
}

// Registry exposes the pure read surface, so a Manager-holding handler reads
// through the same paths the finders do.
func (m *Manager) Registry() *Registry {
	return m.reg
}

// Ensure makes the sdd-owned clone of repo when this machine holds none yet
// and keeps it otherwise, reporting whether a clone ran. A clone the gate
// defers returns its *gitgate.DeferredError. The read-only cache clone an
// earlier sdd kept for repo goes once the clone stands.
func (m *Manager) Ensure(ctx context.Context, repo ConnectedRepo) (dir string, cloned bool, err error) {
	logger := slogutils.FromContext(ctx)
	dir, err = m.clones.Dir(repo.RepoID)
	if err != nil {
		return "", false, err
	}
	if !IsCloned(dir) {
		logger.Debug("cloning connected repo", "repo", repo.RepoID, "url", repo.CloneURL)
	}
	dir, cloned, err = m.clones.EnsureConnected(ctx, repo.RepoID, repo.CloneURL)
	if err != nil {
		return "", false, fmt.Errorf("cloning %s: %w", repo.RepoID, err)
	}
	if cloned {
		logger.Info("cloned connected repo", "repo", repo.RepoID, "url", repo.CloneURL)
	}
	err = m.dropCacheClone(repo)
	if err != nil {
		return "", false, err
	}
	return dir, cloned, nil
}

// Stage clones cloneURL beside the clones, for `sdd repo add` to read the
// repo id it declares before Place moves it (see GraphClones.Stage).
func (m *Manager) Stage(ctx context.Context, cloneURL string) (staged string, remove func(), err error) {
	return m.clones.Stage(ctx, cloneURL)
}

// Place moves a staged clone of repo to where its clone lives.
func (m *Manager) Place(ctx context.Context, staged string, repo ConnectedRepo) (string, error) {
	dir, err := m.clones.Place(ctx, staged, repo.RepoID)
	if err != nil {
		return "", err
	}
	return dir, m.dropCacheClone(repo)
}

// FetchDue reports whether the gate admits a fetch of repo now and when it
// next does: of its clone, or of the first clone while it has none.
func (m *Manager) FetchDue(ctx context.Context, repo ConnectedRepo) (bool, time.Time, error) {
	if m.clones.gate == nil {
		return false, time.Time{}, ErrNoGate
	}
	target, err := m.clones.Target(ctx, repo.RepoID, repo.CloneURL)
	if err != nil {
		return false, time.Time{}, err
	}
	return m.clones.gate.Due(target, gitgate.Fetch)
}

// Slots names the host repo's operations reach and how many of them the
// gate runs against it at once.
func (m *Manager) Slots(ctx context.Context, repo ConnectedRepo) (host string, slots int, err error) {
	if m.clones.gate == nil {
		return "", 0, ErrNoGate
	}
	target, err := m.clones.Target(ctx, repo.RepoID, repo.CloneURL)
	if err != nil {
		return "", 0, err
	}
	slots, err = m.clones.gate.Concurrency(target)
	return target.Host, slots, err
}

// dropCacheClone removes the read-only cache clone an earlier sdd kept for
// repo. A cache still holding a pre-store index stays until `sdd init`
// migrates it; the index stores beside it stay.
func (m *Manager) dropCacheClone(repo ConnectedRepo) error {
	cache, err := m.reg.LegacyCacheDir(repo.RepoID)
	if err != nil {
		return err
	}
	if !IsCloned(cache) {
		return nil
	}
	_, statErr := os.Stat(LegacyIndexDir(cache))
	if statErr == nil {
		return nil
	}
	err = os.RemoveAll(cache)
	if err != nil {
		return fmt.Errorf("removing the former cache clone of %s: %w", repo.RepoID, err)
	}
	return nil
}
