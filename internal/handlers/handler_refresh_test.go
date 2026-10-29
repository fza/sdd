package handlers_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/repos/repostest"
)

// scriptedGit pulls per cache dir: a pull either fails or runs its update;
// clones are not expected.
type scriptedGit struct {
	fail   map[string]error
	update map[string]func()
}

func (g scriptedGit) Clone(context.Context, string, string) error {
	return errors.New("unexpected clone")
}

func (g scriptedGit) PullFFOnly(_ context.Context, dir string) error {
	if err := g.fail[dir]; err != nil {
		return err
	}
	if update := g.update[dir]; update != nil {
		update()
	}
	return nil
}

func writeRepoConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".sdd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".sdd", "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRefreshDependencyClosure walks local → dep → deep with a failing and an
// unconnected sibling. dep's cache holds a broken config that its pull
// repairs, so the walk only succeeds when each repo is refreshed before its
// declarations are read; the failed pull and the missing connection are
// reported and the refresh still succeeds.
func TestRefreshDependencyClosure(t *testing.T) {
	loc := repos.Locations{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), CacheRoot: t.TempDir()}
	reg := repos.NewRegistry(loc)
	connected := &repos.GlobalConfig{}
	cacheDirs := map[model.RepoID]string{}
	for _, repoID := range []model.RepoID{"example.test/dep", "example.test/deep", "example.test/failing"} {
		if err := connected.AddRepo(repos.ConnectedRepo{RepoID: repoID, CloneURL: "https://" + string(repoID)}); err != nil {
			t.Fatal(err)
		}
		dir, err := reg.CacheDir(repoID)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		cacheDirs[repoID] = dir
	}
	repostest.WriteConfig(t, loc.ConfigPath, connected)
	writeRepoConfig(t, cacheDirs["example.test/dep"], "repo_id: [\n")
	writeRepoConfig(t, cacheDirs["example.test/deep"], "repo_id: example.test/deep\n")
	writeRepoConfig(t, cacheDirs["example.test/failing"], "repo_id: example.test/failing\n")

	git := scriptedGit{
		fail: map[string]error{cacheDirs["example.test/failing"]: errors.New("remote unreachable")},
		update: map[string]func(){cacheDirs["example.test/dep"]: func() {
			writeRepoConfig(t, cacheDirs["example.test/dep"], "repo_id: example.test/dep\ndependencies: [example.test/deep]\n")
		}},
	}
	reader := finders.New(finders.Options{Repos: reg, Config: &model.PerRepoConfig{
		RepoID:       "example.test/local",
		Dependencies: []model.RepoID{"example.test/dep", "example.test/failing", "example.test/unconnected"},
	}})
	h := handlers.New(handlers.Options{Reader: reader, Repos: repos.NewManager(reg, git)})

	var failed, unconnected []model.RepoID
	err := h.RefreshDependencyClosure(t.Context(), command.RefreshDependencyClosureCmd{
		OnRefreshFailed: func(repoID model.RepoID, _ error) { failed = append(failed, repoID) },
		OnUnconnected:   func(repoID model.RepoID) { unconnected = append(unconnected, repoID) },
	})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !slices.Equal(failed, []model.RepoID{"example.test/failing"}) {
		t.Errorf("reported refresh failures = %v, want the failing repo", failed)
	}
	if !slices.Equal(unconnected, []model.RepoID{"example.test/unconnected"}) {
		t.Errorf("reported unconnected = %v, want the unconnected repo", unconnected)
	}

	closure, err := reader.DependencyClosure(t.Context(), query.DependencyClosureQuery{})
	if err != nil {
		t.Fatal(err)
	}
	want := []model.RepoID{"example.test/dep", "example.test/failing", "example.test/unconnected", "example.test/deep"}
	if !slices.Equal(closure, want) {
		t.Errorf("closure after refresh = %v, want %v", closure, want)
	}

	if err := h.RefreshDependencyClosure(t.Context(), command.RefreshDependencyClosureCmd{}); err == nil {
		t.Error("a refresh without warning callbacks was accepted")
	}
}
