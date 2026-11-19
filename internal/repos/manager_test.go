package repos_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/syncstate"
	"github.com/networkteam/sdd/test"
)

// A clone under the cache root belongs to an sdd that keeps its clones
// there: the first use of its repository and `sdd repo add` leave it whole.
func TestTheManagerKeepsACloneUnderTheCacheRoot(t *testing.T) {
	const repoID = model.RepoID("example.test/acme/other")
	repo := repos.ConnectedRepo{RepoID: repoID, CloneURL: "https://example.test/acme/other.git"}
	cases := []struct {
		name string
		use  func(t *testing.T, mgr *repos.Manager)
	}{
		{name: "first use", use: func(t *testing.T, mgr *repos.Manager) {
			_, _, err := mgr.Ensure(t.Context(), repo)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "repo add", use: func(t *testing.T, mgr *repos.Manager) {
			staged, remove, err := mgr.Stage(t.Context(), repo.CloneURL)
			if err != nil {
				t.Fatal(err)
			}
			defer remove()
			_, err = mgr.Place(t.Context(), staged, repo)
			if err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loc := testLocations(t)
			mgr := repos.NewManager(repos.NewRegistry(loc), repos.NewGraphClones(loc, &countingGit{}, fixedGate(t), synctest.CloneLocks{}))
			cache := filepath.Join(loc.CacheRoot, filepath.FromSlash(string(repoID)))
			kept := []string{filepath.Join(cache, ".git", "HEAD"), filepath.Join(cache, ".sdd", "config.yaml")}
			for _, path := range kept {
				err := os.MkdirAll(filepath.Dir(path), 0o755)
				if err != nil {
					t.Fatal(err)
				}
				err = os.WriteFile(path, []byte("kept\n"), 0o644)
				if err != nil {
					t.Fatal(err)
				}
			}

			tc.use(t, mgr)

			for _, path := range kept {
				_, statErr := os.Stat(path)
				if statErr != nil {
					t.Errorf("the clone under the cache root should stay whole: %v", statErr)
				}
			}
		})
	}
}

// The clone URL decides the remotes: one of the two forms the repo id derives
// picks the protocol tried first, any other URL is the one remote.
func TestEnsureClonesFromTheConnectedURL(t *testing.T) {
	const repoID = model.RepoID("example.test/acme/other")
	cases := []struct {
		name string
		url  string
		want model.CloneSource
	}{
		{name: "https form", url: "https://example.test/acme/other.git", want: model.CloneSource{First: model.ProtocolHTTPS}},
		{name: "ssh form", url: "git@example.test:acme/other.git", want: model.CloneSource{First: model.ProtocolSSH}},
		{name: "ssh host alias", url: "work:acme/other.git", want: model.CloneSource{URL: "work:acme/other.git"}},
		{name: "mirror", url: "https://mirror.example/acme/other.git", want: model.CloneSource{URL: "https://mirror.example/acme/other.git"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			git := &countingGit{}
			loc := testLocations(t)
			mgr := repos.NewManager(repos.NewRegistry(loc), repos.NewGraphClones(loc, git, fixedGate(t), synctest.CloneLocks{}))
			_, _, err := mgr.Ensure(t.Context(), repos.ConnectedRepo{RepoID: repoID, CloneURL: tc.url})
			if err != nil {
				t.Fatal(err)
			}
			if len(git.sources) != 1 || git.sources[0] != tc.want {
				t.Errorf("the clone should come from %+v, got %+v", tc.want, git.sources)
			}
		})
	}
}

// `sdd repo add` clones before it knows the repo id: Stage clones beside the
// clones, Place moves the clone to its id and keeps one already there.
func TestStageThenPlaceAtTheDeclaredID(t *testing.T) {
	const repoID = model.RepoID("example.test/acme/other")
	git := &countingGit{files: map[string]string{".sdd/config.yaml": "repo_id: example.test/acme/other\n"}}
	loc := testLocations(t)
	mgr := repos.NewManager(repos.NewRegistry(loc), repos.NewGraphClones(loc, git, fixedGate(t), synctest.CloneLocks{}))

	staged, remove, err := mgr.Stage(t.Context(), "/srv/git/other")
	if err != nil {
		t.Fatal(err)
	}
	defer remove()
	cfg, err := repos.Clone{Dir: staged}.Config()
	if err != nil || cfg.RepoID != repoID {
		t.Fatalf("the staged clone should declare its id, got %+v %v", cfg, err)
	}
	dir, err := mgr.Place(t.Context(), staged, repos.ConnectedRepo{RepoID: repoID, CloneURL: "/srv/git/other"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(loc.DataRoot, "repos", "example.test", "acme", "other"); dir != want || !repos.IsCloned(dir) {
		t.Errorf("the clone should be placed at its id, got %s", dir)
	}
	remove()
	if !repos.IsCloned(dir) {
		t.Error("removing the staging leftovers should leave the placed clone")
	}

	again, removeAgain, err := mgr.Stage(t.Context(), "/srv/git/other")
	if err != nil {
		t.Fatal(err)
	}
	defer removeAgain()
	err = os.WriteFile(filepath.Join(dir, "kept"), nil, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.Place(t.Context(), again, repos.ConnectedRepo{RepoID: repoID, CloneURL: "/srv/git/other"})
	if err != nil {
		t.Fatal(err)
	}
	_, statErr := os.Stat(filepath.Join(dir, "kept"))
	if statErr != nil {
		t.Error("a clone already at the id should stay")
	}
}

// Against real git a connected repo reached by a plain path is cloned with
// that one remote and read through the clone.
func TestEnsureClonesAConnectedRepoWithGit(t *testing.T) {
	const repoID = model.RepoID("example.test/acme/other")
	remote := test.NewGraphRemote(t, repoID)
	remote.Commit(t, "seed", map[string]string{
		".sdd/config.yaml":                         "repo_id: example.test/acme/other\ngraph_dir: decisions\n",
		"decisions/2026/06/01-120000-s-tac-abc.md": "---\ntype: signal\n---\nA remote gap.\n",
		"src/code.go":                              "package code\n",
	})
	loc := testLocations(t)
	gate := gitgate.New(gitgate.Options{StateDir: t.TempDir(), Limits: func(string) gitgate.Limits { return model.DefaultGitLimits() }})
	mgr := repos.NewManager(repos.NewRegistry(loc), repos.NewGraphClones(loc, git.GraphClones{}, gate, syncstate.CloneLocks{}))

	dir, cloned, err := mgr.Ensure(t.Context(), repos.ConnectedRepo{RepoID: repoID, CloneURL: remote.Bare})
	if err != nil || !cloned {
		t.Fatalf("Ensure: %v %v", cloned, err)
	}
	graphDir, err := repos.Clone{RepoID: repoID, Dir: dir}.GraphDir()
	if err != nil {
		t.Fatal(err)
	}
	_, statErr := os.Stat(filepath.Join(graphDir, "2026", "06", "01-120000-s-tac-abc.md"))
	if statErr != nil {
		t.Errorf("the graph outside .sdd should be checked out: %v", statErr)
	}
	_, statErr = os.Stat(filepath.Join(dir, "src"))
	if statErr == nil {
		t.Error("code outside the cone should stay out")
	}
	if remotes := test.Git(t, dir, "remote"); remotes != model.SingleRemote+"\n" {
		t.Errorf("a plain path should be the clone's one remote, got %q", remotes)
	}
}
