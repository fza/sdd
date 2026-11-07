package repos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/gitgate"
)

// Git is the narrow git surface the cache lifecycle needs. The production
// implementation is internal/git's exec adapter; tests inject fakes.
type Git interface {
	// Clone clones url into dir.
	Clone(ctx context.Context, url, dir string) error
	// PullFFOnly pulls the checkout at dir, fast-forward only — the cache is
	// read-only, so a non-ff state means the remote rewrote history; surface
	// that rather than merging.
	PullFFOnly(ctx context.Context, dir string) error
	// ExpandURL applies git's URL rewrites to url without reaching it.
	ExpandURL(ctx context.Context, url string) (string, error)
}

// ErrNoGate reports a git network operation asked of a component built
// without the gate: a wiring gap, never a user state.
var ErrNoGate = errors.New("git network operation without the git gate")

// Manager owns the side-effectful half of the connected-repos machinery:
// cache clone and pull, and config writes. It wraps the pure Registry and is
// the dependency handlers get injected — the write-side counterpart to the
// finder-injected Registry. Every clone and pull passes the gate as a fetch
// (20261107-095427-d-tac-yux).
type Manager struct {
	reg  *Registry
	git  Git
	gate *gitgate.Gate
}

// NewManager builds a Manager over the registry, the git dependency and the
// gate its clones and pulls pass.
func NewManager(reg *Registry, git Git, gate *gitgate.Gate) *Manager {
	return &Manager{reg: reg, git: git, gate: gate}
}

// Registry exposes the pure read surface, so a Manager-holding handler reads
// through the same paths the finders do.
func (m *Manager) Registry() *Registry {
	return m.reg
}

// EnsureCloned clones repo.CloneURL into dir when no clone is present yet.
// Reports whether a clone ran (false: the cache already existed). A clone the
// gate defers returns its *gitgate.DeferredError.
func (m *Manager) EnsureCloned(ctx context.Context, repo ConnectedRepo, dir string) (bool, error) {
	if IsCloned(dir) {
		return false, nil
	}
	logger := slogutils.FromContext(ctx)
	// On repo add the repo ID is unknown until the clone is inspected.
	attrs := []any{"url", repo.CloneURL}
	if repo.RepoID != "" {
		attrs = append([]any{"repo", repo.RepoID}, attrs...)
	}
	name := string(repo.RepoID)
	if name == "" {
		name = repo.CloneURL
	}
	logger.Debug("cloning connected repo", attrs...)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return false, fmt.Errorf("creating cache dir: %w", err)
	}
	err := m.fetch(ctx, repo, func(ctx context.Context) error {
		return m.git.Clone(ctx, repo.CloneURL, dir)
	})
	if err != nil {
		return false, fmt.Errorf("cloning %s: %w", name, err)
	}
	logger.Info("cloned connected repo", attrs...)
	return true, nil
}

// Pull pulls the cache of repo at dir (fast-forward only) when the gate
// admits it; a deferred pull returns its *gitgate.DeferredError.
func (m *Manager) Pull(ctx context.Context, repo ConnectedRepo, dir string) error {
	if !IsCloned(dir) {
		return fmt.Errorf("no clone at %s", dir)
	}
	slogutils.FromContext(ctx).Debug("pulling connected repo cache", "dir", dir)
	err := m.fetch(ctx, repo, func(ctx context.Context) error {
		return m.git.PullFFOnly(ctx, dir)
	})
	if err != nil {
		return fmt.Errorf("pulling cache %s: %w", dir, err)
	}
	return nil
}

// FetchDue reports whether the gate would admit a clone or pull of repo now,
// so a caller can label the phase before it runs.
func (m *Manager) FetchDue(ctx context.Context, repo ConnectedRepo) (bool, error) {
	if m.gate == nil {
		return false, ErrNoGate
	}
	target, err := m.target(ctx, repo)
	if err != nil {
		return false, err
	}
	due, _, err := m.gate.Due(target, gitgate.Fetch)
	return due, err
}

func (m *Manager) fetch(ctx context.Context, repo ConnectedRepo, op func(ctx context.Context) error) error {
	if m.gate == nil {
		return ErrNoGate
	}
	target, err := m.target(ctx, repo)
	if err != nil {
		return err
	}
	return m.gate.Run(ctx, target, gitgate.Fetch, op)
}

func (m *Manager) target(ctx context.Context, repo ConnectedRepo) (gitgate.Target, error) {
	url, err := m.git.ExpandURL(ctx, repo.CloneURL)
	if err != nil {
		return gitgate.Target{}, err
	}
	return gitgate.TargetFromURL(url)
}
