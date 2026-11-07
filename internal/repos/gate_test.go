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
// network operations that reach it and clones by creating .git plus files.
type countingGit struct {
	clones, pushes int
	sources        []model.CloneSource
	files          map[string]string
	// remote is the URL a clone's remote has; empty is the graph repository
	// of the graph clone tests.
	remote string
}

func (g *countingGit) ExpandURL(_ context.Context, url string) (string, error) { return url, nil }

func (g *countingGit) CloneGraph(_ context.Context, _ model.RepoID, source model.CloneSource, dir string, _ []string) error {
	g.clones++
	g.sources = append(g.sources, source)
	err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	if err != nil {
		return err
	}
	for name, content := range g.files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		err = os.MkdirAll(filepath.Dir(path), 0o755)
		if err != nil {
			return err
		}
		err = os.WriteFile(path, []byte(content), 0o644)
		if err != nil {
			return err
		}
	}
	return nil
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
	if g.remote != "" {
		return g.remote, nil
	}
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

func testLocations(t *testing.T) repos.Locations {
	t.Helper()
	return repos.Locations{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), CacheRoot: t.TempDir(), DataRoot: t.TempDir()}
}

func TestManagerClonesThroughTheGate(t *testing.T) {
	git := &countingGit{remote: "https://example.test/acme/other.git"}
	loc := testLocations(t)
	mgr := repos.NewManager(repos.NewRegistry(loc), repos.NewGraphClones(loc, git, fixedGate(t)))
	repo := repos.ConnectedRepo{RepoID: "example.test/acme/other", CloneURL: "https://example.test/acme/other.git"}

	due, _, err := mgr.FetchDue(t.Context(), repo)
	if err != nil || !due {
		t.Fatalf("the first clone should be due, got %v %v", due, err)
	}
	dir, cloned, err := mgr.Ensure(t.Context(), repo)
	if err != nil || !cloned {
		t.Fatalf("the first use should clone, got %v %v", cloned, err)
	}
	if want := filepath.Join(loc.DataRoot, "repos", "example.test", "acme", "other"); dir != want {
		t.Errorf("the clone should live at its id under the data root, got %s", dir)
	}
	_, cloned, err = mgr.Ensure(t.Context(), repo)
	if err != nil || cloned || git.clones != 1 {
		t.Fatalf("a second use should keep the clone, got %v %v after %d clones", cloned, err, git.clones)
	}
	due, next, err := mgr.FetchDue(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if due || !next.After(time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("a fetch right after the clone should wait for its cooldown, got due %v next %v", due, next)
	}
}

func TestManagerWithoutGateRefusesToClone(t *testing.T) {
	git := &countingGit{}
	loc := testLocations(t)
	mgr := repos.NewManager(repos.NewRegistry(loc), repos.NewGraphClones(loc, git, nil))
	repo := repos.ConnectedRepo{RepoID: "example.test/acme/other", CloneURL: "https://example.test/acme/other.git"}

	_, _, err := mgr.Ensure(t.Context(), repo)
	if !errors.Is(err, repos.ErrNoGate) || git.clones != 0 {
		t.Errorf("a manager without a gate should refuse the clone, got %v", err)
	}
	_, _, err = mgr.FetchDue(t.Context(), repo)
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
