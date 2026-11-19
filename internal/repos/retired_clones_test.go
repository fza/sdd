package repos_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

func letGo(string) (func(), error) { return func() {}, nil }

func cloneLocations(t *testing.T) repos.Locations {
	t.Helper()
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	writeFile(t, loc.ConfigPath, keptRepo)
	return loc
}

func makeClone(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func ownedCloneDir(loc repos.Locations, repoID model.RepoID) string {
	return filepath.Join(loc.DataRoot, "repos", filepath.FromSlash(string(repoID)))
}

// A clone named like one an earlier run retired goes only by the rules every
// sdd-owned clone goes by, and a clone under the cache root is never listed,
// whatever the record of its repository says.
func TestUnconnectedClonesJudgeEveryOwnedCloneByItsRecord(t *testing.T) {
	loc := cloneLocations(t)
	recorded := ownedCloneDir(loc, "github.com/acme/dropped")
	for _, dir := range []string{
		recorded,
		filepath.Join(loc.CacheRoot, "github.com", "acme", "dropped"),
		filepath.Join(loc.CacheRoot, "github.com", "acme", "unrecorded"),
		ownedCloneDir(loc, "github.com/acme/.old.pruned"),
	} {
		makeClone(t, dir)
	}
	registry := repos.NewRegistry(loc)
	if err := repos.NewManager(registry, nil).MarkDisconnected("github.com/acme/dropped"); err != nil {
		t.Fatal(err)
	}
	stale, err := registry.UnconnectedClones()
	if err != nil {
		t.Fatal(err)
	}
	want := []repos.UnconnectedClone{{Dir: recorded, RepoID: "github.com/acme/dropped"}}
	if !slices.Equal(stale, want) {
		t.Errorf("only the sdd-owned clone with a record should be listed, got %+v", stale)
	}
}

func TestRetireUnconnectedCloneKeepsAConnectedCloneNamedLikeARetiredOne(t *testing.T) {
	loc := cloneLocations(t)
	writeFile(t, loc.ConfigPath, "repos:\n  - repo_id: github.com/acme/.kept.pruned\n    clone_url: git@github.com:acme/.kept.pruned.git\n")
	clone := repos.UnconnectedClone{Dir: ownedCloneDir(loc, "github.com/acme/.kept.pruned"), RepoID: "github.com/acme/.kept.pruned"}
	makeClone(t, clone.Dir)
	retired, err := repos.NewRegistry(loc).RetireUnconnectedClone(clone, letGo)
	if err != nil {
		t.Fatal(err)
	}
	if retired != "" {
		t.Errorf("a connected clone should not be handed out for deletion, got %s", retired)
	}
}

func TestRetireUnconnectedCloneRejudgesUnderTheLock(t *testing.T) {
	cases := []struct {
		name    string
		change  func(t *testing.T, loc repos.Locations, registry *repos.Registry)
		refused bool
	}{
		{
			name: "a record cleared since the listing",
			change: func(t *testing.T, _ repos.Locations, registry *repos.Registry) {
				if err := repos.NewManager(registry, nil).ClearDisconnected("github.com/acme/dropped"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "a repos list emptied since the listing", refused: true,
			change: func(t *testing.T, loc repos.Locations, _ *repos.Registry) {
				writeFile(t, loc.ConfigPath, "repos: []\n")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loc := cloneLocations(t)
			clone := repos.UnconnectedClone{Dir: ownedCloneDir(loc, "github.com/acme/dropped"), RepoID: "github.com/acme/dropped"}
			makeClone(t, clone.Dir)
			registry := repos.NewRegistry(loc)
			if err := repos.NewManager(registry, nil).MarkDisconnected(clone.RepoID); err != nil {
				t.Fatal(err)
			}
			tc.change(t, loc, registry)
			retired, err := registry.RetireUnconnectedClone(clone, letGo)
			if refused := errors.Is(err, repos.ErrClonePassRefused); refused != tc.refused {
				t.Errorf("the retirement should be refused only under an unsure repos list, err %v", err)
			}
			if !tc.refused && err != nil {
				t.Fatal(err)
			}
			if retired != "" || !repos.IsCloned(clone.Dir) {
				t.Errorf("a clone that no longer meets the rules should stay, retired to %q", retired)
			}
		})
	}
}

func TestRetiredClonesWaitWhereNoRepoIDReaches(t *testing.T) {
	loc := cloneLocations(t)
	clones := []repos.UnconnectedClone{
		{Dir: ownedCloneDir(loc, "github.com/acme/dropped"), RepoID: "github.com/acme/dropped"},
		{Dir: ownedCloneDir(loc, "github.com/acme/other"), RepoID: "github.com/acme/other"},
	}
	registry := repos.NewRegistry(loc)
	for _, clone := range clones {
		makeClone(t, clone.Dir)
		if err := repos.NewManager(registry, nil).MarkDisconnected(clone.RepoID); err != nil {
			t.Fatal(err)
		}
	}
	var retired []string
	for _, clone := range clones {
		dir, err := registry.RetireUnconnectedClone(clone, letGo)
		if err != nil {
			t.Fatal(err)
		}
		if dir == "" {
			t.Fatalf("a recorded clone of a repo that is gone from the list should be retired: %s", clone.Dir)
		}
		rel, err := filepath.Rel(filepath.Join(loc.DataRoot, "repos"), dir)
		if err != nil || !filepath.IsLocal(rel) || model.ValidateRepoID(model.RepoID(filepath.ToSlash(rel))) == nil {
			t.Errorf("a retired clone should wait under the sdd-owned clones where no repo ID places a clone, got %s", dir)
		}
		retired = append(retired, dir)
	}
	stale, err := registry.UnconnectedClones()
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, clone := range stale {
		listed = append(listed, clone.Dir)
	}
	if !slices.Equal(listed, retired) {
		t.Fatalf("retired clones a run left behind should be listed for the next run, got %+v", stale)
	}
	for _, clone := range stale {
		again, err := registry.RetireUnconnectedClone(clone, letGo)
		if err != nil {
			t.Fatal(err)
		}
		if again != clone.Dir {
			t.Errorf("a retired leftover should be handed back for deletion, got %q", again)
		}
	}
}

func TestRetireUnconnectedCloneOfALongRepoID(t *testing.T) {
	loc := cloneLocations(t)
	segment := strings.Repeat("a", 90)
	repoID := model.RepoID("git.example.com/" + segment + "/" + segment + "/" + segment)
	clone := repos.UnconnectedClone{Dir: ownedCloneDir(loc, repoID), RepoID: repoID}
	makeClone(t, clone.Dir)
	registry := repos.NewRegistry(loc)
	if err := repos.NewManager(registry, nil).MarkDisconnected(repoID); err != nil {
		t.Fatal(err)
	}
	retired, err := registry.RetireUnconnectedClone(clone, letGo)
	if err != nil {
		t.Fatal(err)
	}
	if retired == "" || len(filepath.Base(retired)) > 255 {
		t.Errorf("a repo ID longer than a file name should still be recorded and retired, got %q", retired)
	}
}
