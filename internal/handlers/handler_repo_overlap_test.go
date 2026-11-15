package handlers_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/repos/repostest"
)

// declaringGit clones a repository whose committed config declares the
// repo_id mapped to its URL; nothing reaches a remote.
type declaringGit struct{ declares map[string]model.RepoID }

func (g declaringGit) CloneGraph(_ context.Context, _ model.RepoID, source model.CloneSource, dir string, _ []string) error {
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, ".sdd"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ".sdd", "config.yaml"), []byte("repo_id: "+string(g.declares[source.URL])+"\n"), 0o644)
}
func (declaringGit) SparseCone(context.Context, string) ([]string, error) {
	return model.GraphCloneCone(""), nil
}
func (declaringGit) SetSparseCone(context.Context, string, []string) error  { return nil }
func (declaringGit) KeepGraphClone(context.Context, string, []string) error { return nil }
func (declaringGit) PushGraphClone(context.Context, string) error           { return nil }
func (declaringGit) CommitsWaiting(context.Context, string) (bool, error)   { return false, nil }
func (declaringGit) RemoteURL(context.Context, string) (string, error)      { return "/srv/remote", nil }
func (declaringGit) ExpandURL(_ context.Context, url string) (string, error) {
	return url, nil
}

type alwaysDueRound struct{}

func (alwaysDueRound) SyncClone(context.Context, string, *command.SyncRoundCmd) error { return nil }
func (alwaysDueRound) TimerRound(context.Context, string) (model.TimerRound, error) {
	return model.TimerRoundSync, nil
}

func overlapLocations(t *testing.T) repos.Locations {
	t.Helper()
	root := t.TempDir()
	return repos.Locations{
		ConfigPath: filepath.Join(root, "config", "sdd", "config.yaml"),
		CacheRoot:  filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data"),
	}
}

func overlapHandler(t *testing.T, loc repos.Locations, git declaringGit) *handlers.Handler {
	t.Helper()
	gate := synctest.Gate(loc.StateRoot, nil)
	manager := repos.NewManager(repos.NewRegistry(loc), repos.NewGraphClones(loc, git, gate))
	return handlers.New(handlers.Options{Repos: manager, CloneRound: alwaysDueRound{}})
}

// overlapping is a repos list connected before overlap was checked: one repo
// nested in another, beside a disjoint one.
var overlapping = &repos.GlobalConfig{Repos: []repos.ConnectedRepo{
	{RepoID: "github.com/org", CloneURL: "/srv/org"},
	{RepoID: "github.com/org/repo", CloneURL: "/srv/repo"},
	{RepoID: "github.com/other/repo", CloneURL: "/srv/other"},
}}

func TestRepoAdd_RefusesAnIDOverlappingAConnectedClone(t *testing.T) {
	for _, declared := range []model.RepoID{"github.com/ORG/repo", "github.com/org", "github.com/org/repo/sub"} {
		t.Run(string(declared), func(t *testing.T) {
			loc := overlapLocations(t)
			cfg := &repos.GlobalConfig{}
			if err := cfg.AddRepo(repos.ConnectedRepo{RepoID: "github.com/org/repo", CloneURL: "git@github.com:org/repo.git"}); err != nil {
				t.Fatal(err)
			}
			repostest.WriteConfig(t, loc.ConfigPath, cfg)
			clone, err := repos.NewRegistry(loc).CloneDir("github.com/org/repo")
			if err != nil {
				t.Fatal(err)
			}
			precious := filepath.Join(clone, ".git", "precious")
			if err := os.MkdirAll(filepath.Dir(precious), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(precious, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			const url = "/srv/crafted"
			h := overlapHandler(t, loc, declaringGit{declares: map[string]model.RepoID{url: declared}})

			err = h.RepoAdd(t.Context(), &command.RepoAddCmd{CloneURL: url})
			if err == nil || !strings.Contains(err.Error(), "overlaps") {
				t.Errorf("adding an id overlapping a connected repo should be refused, got %v", err)
			}
			if _, statErr := os.Stat(precious); statErr != nil {
				t.Errorf("the connected clone should stay untouched: %v", statErr)
			}
			loaded, err := repos.NewRegistry(loc).Load()
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded.Repos) != 1 {
				t.Errorf("the refused repo should not be connected, repos %+v", loaded.Repos)
			}
		})
	}
}

func TestRepoSync_RefusesOverlappingReposAndSyncsTheRest(t *testing.T) {
	loc := overlapLocations(t)
	repostest.WriteConfig(t, loc.ConfigPath, overlapping)
	h := overlapHandler(t, loc, declaringGit{})

	states := map[model.RepoID]model.RepoSyncState{}
	reasons := map[model.RepoID]string{}
	err := h.RepoSync(t.Context(), &command.RepoSyncCmd{OnOutcome: func(o model.RepoSyncOutcome) {
		states[o.RepoID] = o.State
		reasons[o.RepoID] = o.Reason
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []model.RepoID{"github.com/org", "github.com/org/repo"} {
		if states[id] != model.RepoRefused || !strings.Contains(reasons[id], "overlaps") {
			t.Errorf("a repo of an overlapping pair should be refused naming the overlap, got %s %q", states[id], reasons[id])
		}
	}
	if states["github.com/other/repo"] == model.RepoRefused || states["github.com/other/repo"] == model.RepoFailed {
		t.Errorf("the disjoint repo should sync, got %s %q", states["github.com/other/repo"], reasons["github.com/other/repo"])
	}
	assertClonedOnlyDisjoint(t, loc)
}

func TestEnsureReposFresh_SkipsOverlappingReposAndRefreshesTheRest(t *testing.T) {
	loc := overlapLocations(t)
	repostest.WriteConfig(t, loc.ConfigPath, overlapping)
	h := overlapHandler(t, loc, declaringGit{})

	_, err := h.EnsureReposFresh(t.Context(), command.EnsureReposFreshCmd{
		RepoIDs: []model.RepoID{"github.com/org", "github.com/org/repo", "github.com/other/repo"},
	})
	if err == nil || !strings.Contains(err.Error(), `"github.com/org"`) || !strings.Contains(err.Error(), `"github.com/org/repo"`) {
		t.Errorf("the refresh should fail naming each side of the overlapping pair, got %v", err)
	}
	assertClonedOnlyDisjoint(t, loc)
}

func assertClonedOnlyDisjoint(t *testing.T, loc repos.Locations) {
	t.Helper()
	for id, want := range map[model.RepoID]bool{"github.com/org": false, "github.com/org/repo": false, "github.com/other/repo": true} {
		dir, err := repos.NewRegistry(loc).CloneDir(id)
		if err != nil {
			t.Fatal(err)
		}
		if repos.IsCloned(dir) != want {
			t.Errorf("%s should be cloned only when it overlaps no other repo", id)
		}
	}
}
