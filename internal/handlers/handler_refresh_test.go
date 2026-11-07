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
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/repos/repostest"
)

// scriptedRound runs a round per clone dir: it either fails or runs its
// update.
type scriptedRound struct {
	fail   map[string]error
	update map[string]func()
}

func (r scriptedRound) SyncClone(_ context.Context, dir string, _ *command.SyncRoundCmd) error {
	err := r.fail[dir]
	if err != nil {
		return err
	}
	if update := r.update[dir]; update != nil {
		update()
	}
	return nil
}

func (r scriptedRound) Due(context.Context, string) (bool, error) { return true, nil }

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
// unconnected sibling. dep's clone holds a broken config that its round
// repairs, so the walk only succeeds when each repo is refreshed before its
// declarations are read; the failed round and the missing connection are
// reported and the refresh still succeeds.
func TestRefreshDependencyClosure(t *testing.T) {
	loc := repos.Locations{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), CacheRoot: t.TempDir(), DataRoot: t.TempDir()}
	reg := repos.NewRegistry(loc)
	connected := &repos.GlobalConfig{}
	cloneDirs := map[model.RepoID]string{}
	for _, repoID := range []model.RepoID{"example.test/dep", "example.test/deep", "example.test/failing"} {
		if err := connected.AddRepo(repos.ConnectedRepo{RepoID: repoID, CloneURL: "https://" + string(repoID)}); err != nil {
			t.Fatal(err)
		}
		dir, err := reg.CloneDir(repoID)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		cloneDirs[repoID] = dir
	}
	repostest.WriteConfig(t, loc.ConfigPath, connected)
	writeRepoConfig(t, cloneDirs["example.test/dep"], "repo_id: [\n")
	writeRepoConfig(t, cloneDirs["example.test/deep"], "repo_id: example.test/deep\n")
	writeRepoConfig(t, cloneDirs["example.test/failing"], "repo_id: example.test/failing\n")

	round := scriptedRound{
		fail: map[string]error{cloneDirs["example.test/failing"]: errors.New("remote unreachable")},
		update: map[string]func(){cloneDirs["example.test/dep"]: func() {
			writeRepoConfig(t, cloneDirs["example.test/dep"], "repo_id: example.test/dep\ndependencies: [example.test/deep]\n")
		}},
	}
	reader := finders.New(finders.Options{Repos: reg, Config: &model.PerRepoConfig{
		RepoID:       "example.test/local",
		Dependencies: []model.RepoID{"example.test/dep", "example.test/failing", "example.test/unconnected"},
	}})
	manager := repos.NewManager(reg, repos.NewGraphClones(loc, &synctest.CloneGit{}, synctest.Gate(t.TempDir(), nil)))
	h := handlers.New(handlers.Options{Reader: reader, Repos: manager, CloneRound: round})

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
