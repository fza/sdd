package repos_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

// countingGit stands in for git against a remote on a host: it counts the
// network operations that reach it and clones by creating .git.
type countingGit struct {
	clones, pulls, pushes int
}

func (g *countingGit) Clone(_ context.Context, _, dir string) error {
	g.clones++
	return os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
}

func (g *countingGit) PullFFOnly(context.Context, string) error {
	g.pulls++
	return nil
}

func (g *countingGit) ExpandURL(_ context.Context, url string) (string, error) { return url, nil }

func (g *countingGit) CloneGraph(_ context.Context, _ model.RepoID, _, dir string, _ []string) error {
	g.clones++
	return os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
}

func (g *countingGit) SparseCone(context.Context, string) ([]string, error) {
	return model.GraphCloneCone(model.DefaultGraphDir), nil
}

func (g *countingGit) SetSparseCone(context.Context, string, []string) error { return nil }

func (g *countingGit) KeepGraphClone(context.Context, string, []string) error { return nil }

func (g *countingGit) PushGraphClone(context.Context, string) error {
	g.pushes++
	return nil
}

func (g *countingGit) RemoteURL(context.Context, string) (string, error) {
	return "git@example.test:acme/app-sdd.git", nil
}

func fixedGate(t *testing.T) *gitgate.Gate {
	t.Helper()
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	return gitgate.New(gitgate.Options{
		StateDir: t.TempDir(),
		Limits:   func(string) gitgate.Limits { return model.DefaultGitLimits() },
		Now:      func() time.Time { return now },
	})
}

func TestManagerPullsThroughTheGate(t *testing.T) {
	git := &countingGit{}
	loc := repos.Locations{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), CacheRoot: t.TempDir()}
	mgr := repos.NewManager(repos.NewRegistry(loc), git, fixedGate(t))
	repo := repos.ConnectedRepo{RepoID: "example.test/acme/other", CloneURL: "https://example.test/acme/other.git"}
	dir := filepath.Join(loc.CacheRoot, "other")

	cloned, err := mgr.EnsureCloned(t.Context(), repo, dir)
	if err != nil || !cloned {
		t.Fatalf("the first clone should run, got %v %v", cloned, err)
	}
	due, err := mgr.FetchDue(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if due {
		t.Error("a fetch right after the clone should not be due")
	}
	err = mgr.Pull(t.Context(), repo, dir)
	var deferred *gitgate.DeferredError
	if !errors.As(err, &deferred) || deferred.Reason != gitgate.ReasonCooldown {
		t.Fatalf("a pull within the fetch cooldown should be deferred, got %v", err)
	}
	if git.pulls != 0 {
		t.Error("a deferred pull should not reach git")
	}
}

func TestManagerWithoutGateRefusesNetworkOperations(t *testing.T) {
	git := &countingGit{}
	loc := repos.Locations{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), CacheRoot: t.TempDir()}
	mgr := repos.NewManager(repos.NewRegistry(loc), git, nil)
	repo := repos.ConnectedRepo{RepoID: "example.test/acme/other", CloneURL: "https://example.test/acme/other.git"}

	_, err := mgr.EnsureCloned(t.Context(), repo, filepath.Join(loc.CacheRoot, "other"))
	if !errors.Is(err, repos.ErrNoGate) || git.clones != 0 {
		t.Errorf("a manager without a gate should refuse the clone, got %v", err)
	}
	_, err = mgr.FetchDue(t.Context(), repo)
	if !errors.Is(err, repos.ErrNoGate) {
		t.Errorf("a manager without a gate should not claim a fetch due, got %v", err)
	}
}

func TestGraphClonesPassTheGate(t *testing.T) {
	git := &countingGit{}
	clones := repos.NewGraphClones(repos.Locations{DataRoot: t.TempDir()}, git, fixedGate(t))
	repoID := model.RepoID("example.test/acme/app-sdd")

	_, cloned, err := clones.Ensure(t.Context(), repoID, "git@example.test:acme/app.git")
	if err != nil || !cloned {
		t.Fatalf("the first clone should run, got %v %v", cloned, err)
	}
	err = clones.Publish(t.Context(), repoID)
	if err != nil {
		t.Fatal(err)
	}
	err = clones.Publish(t.Context(), repoID)
	var deferred *gitgate.DeferredError
	if !errors.As(err, &deferred) || deferred.Kind != gitgate.Push {
		t.Fatalf("a publish within the push cooldown should be deferred, got %v", err)
	}
	if git.pushes != 1 {
		t.Error("a deferred publish should not reach git")
	}
}

func TestGraphClonesWithoutGateRefuseToClone(t *testing.T) {
	git := &countingGit{}
	clones := repos.NewGraphClones(repos.Locations{DataRoot: t.TempDir()}, git, nil)

	_, _, err := clones.Ensure(t.Context(), "example.test/acme/app-sdd", "")
	if !errors.Is(err, repos.ErrNoGate) || git.clones != 0 {
		t.Errorf("graph clones without a gate should refuse the clone, got %v", err)
	}
}
