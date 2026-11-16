package repos_test

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

// otherRepo is a trusted repos list that names none of the clones under test.
const otherRepo = "repos:\n  - repo_id: github.com/acme/other\n    clone_url: git@github.com:acme/other.git\n"

func markDisconnected(t *testing.T, registry *repos.Registry, repoID model.RepoID) {
	t.Helper()
	if err := repos.NewManager(registry, nil).MarkDisconnected(repoID); err != nil {
		t.Fatal(err)
	}
}

func TestUnconnectedClones(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	config := "repos:\n  - repo_id: github.com/acme/kept\n    clone_url: git@github.com:acme/kept.git\n"
	if err := os.WriteFile(loc.ConfigPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(loc.CacheRoot, "github.com", "acme", "kept")
	dropped := filepath.Join(loc.CacheRoot, "gitlab.example.org", "team", "dropped")
	for _, dir := range []string{
		filepath.Join(kept, ".git"),
		filepath.Join(dropped, ".git"),
		filepath.Join(loc.CacheRoot, "gitlab.example.org", "team", ".old.pruned", ".git"),
		filepath.Join(loc.CacheRoot, "gitlab.example.org", "team", "unlisted", ".git"),
		filepath.Join(loc.CacheRoot, "index", "github.com", "acme", "x", ".git"),
		filepath.Join(loc.CacheRoot, ".adding-123", "clone", ".git"),
		filepath.Join(loc.CacheRoot, "github.com", "acme", "nogit"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	registry := repos.NewRegistry(loc)
	markDisconnected(t, registry, "gitlab.example.org/team/dropped")
	markDisconnected(t, registry, "github.com/acme/kept")

	stale, err := registry.UnconnectedClones()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(dirsOf(stale), []string{dropped}) {
		t.Errorf("only clones repo remove dropped that no connection names should be listed, got %v", stale)
	}
}

func TestUnconnectedClonesKeepAClonesNamedLikeARetiredOne(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	config := "repos:\n  - repo_id: github.com/acme/.kept.pruned\n    clone_url: git@github.com:acme/.kept.pruned.git\n"
	if err := os.WriteFile(loc.ConfigPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{
		filepath.Join(loc.CacheRoot, "github.com", "acme", ".kept.pruned", ".git"),
		filepath.Join(loc.CacheRoot, "github.com", "acme", ".unrecorded.pruned", ".git"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stale, err := repos.NewRegistry(loc).UnconnectedClones()
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Errorf("a clone named like a retired one should go only by the rules every clone goes by, listed %v", stale)
	}
}

func TestUnconnectedClonesListCloneLeftoversARetirementLeft(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	if err := os.WriteFile(loc.ConfigPath, []byte(otherRepo), 0o600); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(loc.CacheRoot, "github.com", "acme", "dropped")
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	registry := repos.NewRegistry(loc)
	markDisconnected(t, registry, "github.com/acme/dropped")
	retired, err := retireDir(registry, clone)
	if err != nil {
		t.Fatal(err)
	}
	if retired == "" {
		t.Fatal("a dropped, unconnected clone should be retired")
	}
	stale, err := registry.UnconnectedClones()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(dirsOf(stale), []string{retired}) {
		t.Errorf("a retired clone a run left behind should be listed for the next run, got %v", stale)
	}
	again, err := retireDir(registry, retired)
	if err != nil {
		t.Fatal(err)
	}
	if again != retired {
		t.Errorf("a retired leftover should be handed back for deletion, got %q", again)
	}
}

func TestDisconnectionRecordHoldsOnlyForItsConfig(t *testing.T) {
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "cache")
	own := repos.Locations{ConfigPath: filepath.Join(root, "real", "config.yaml"), CacheRoot: cacheRoot, StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	other := repos.Locations{ConfigPath: filepath.Join(root, "other", "config.yaml"), CacheRoot: cacheRoot, StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	config := "repos:\n  - repo_id: github.com/acme/kept\n    clone_url: git@github.com:acme/kept.git\n"
	for _, path := range []string{own.ConfigPath, other.ConfigPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	clone := filepath.Join(cacheRoot, "github.com", "acme", "dropped")
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	markDisconnected(t, repos.NewRegistry(own), "github.com/acme/dropped")

	registry := repos.NewRegistry(other)
	dropped, err := registry.Disconnected("github.com/acme/dropped")
	if err != nil {
		t.Fatal(err)
	}
	if dropped {
		t.Error("a disconnection recorded under one config should not hold under another")
	}
	stale, err := registry.UnconnectedClones()
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Errorf("another config's repos list should remove no clone, listed %v", stale)
	}
	retired, err := retireDir(registry, clone)
	if err != nil {
		t.Fatal(err)
	}
	if retired != "" {
		t.Errorf("another config's repos list should retire no clone, retired to %s", retired)
	}
	if stale, err := repos.NewRegistry(own).UnconnectedClones(); err != nil || !slices.Equal(dirsOf(stale), []string{clone}) {
		t.Errorf("the config the disconnection was recorded under should still list the clone, got %v, %v", stale, err)
	}
}

func TestUnconnectedClonesRefuseAnUnsureRepoList(t *testing.T) {
	empty := "repos: []\n"
	listed := "repos:\n  - repo_id: github.com/acme/kept\n    clone_url: git@github.com:acme/kept.git\n"
	cases := []struct {
		name    string
		config  *string
		mixed   string
		dropped bool
		refused bool
	}{
		{name: "a missing config file with a dropped clone", dropped: true, refused: true},
		{name: "an empty repos list with a dropped clone", config: &empty, dropped: true, refused: true},
		{name: "mixed XDG roots with a dropped clone", config: &listed, mixed: "XDG_CACHE_HOME set, XDG_CONFIG_HOME, XDG_STATE_HOME not", dropped: true, refused: true},
		{name: "a missing config file with no clone to remove"},
		{name: "a trusted repos list with a dropped clone", config: &listed, dropped: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data"), MixedXDG: tc.mixed}
			if tc.config != nil {
				if err := os.WriteFile(loc.ConfigPath, []byte(*tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Join(loc.CacheRoot, "github.com", "acme", "graph", ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			registry := repos.NewRegistry(loc)
			if tc.dropped {
				markDisconnected(t, registry, "github.com/acme/graph")
			}
			_, err := registry.UnconnectedClones()
			if refused := errors.Is(err, repos.ErrClonePassRefused); refused != tc.refused {
				t.Errorf("the clone pass should refuse only an unsure repos list with clones to remove, refused = %v, err %v", refused, err)
			}
			if !tc.refused && err != nil {
				t.Errorf("listing should succeed, got %v", err)
			}
		})
	}
}

func TestDefaultLocationsNameMixedXDGRoots(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache")
	t.Setenv("XDG_STATE_HOME", "")
	loc, err := repos.DefaultLocations()
	if err != nil {
		t.Fatal(err)
	}
	if loc.MixedXDG == "" {
		t.Error("locations from mixed XDG sources should say so")
	}
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	loc, err = repos.DefaultLocations()
	if err != nil {
		t.Fatal(err)
	}
	if loc.MixedXDG != "" {
		t.Errorf("locations from one source should not read as mixed, got %q", loc.MixedXDG)
	}
}

func TestRetireUnconnectedClone(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	if err := os.WriteFile(loc.ConfigPath, []byte(otherRepo), 0o600); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(loc.CacheRoot, "github.com", "acme", "readded")
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	registry := repos.NewRegistry(loc)

	t.Run("a clone no repo remove dropped stays", func(t *testing.T) {
		retired, err := retireDir(registry, clone)
		if err != nil {
			t.Fatal(err)
		}
		if retired != "" {
			t.Errorf("a clone without a disconnection record should not be retired, retired to %s", retired)
		}
	})

	markDisconnected(t, registry, "github.com/acme/readded")

	t.Run("a clone connected since it was listed stays", func(t *testing.T) {
		config := "repos:\n  - repo_id: github.com/acme/readded\n    clone_url: git@github.com:acme/readded.git\n"
		if err := os.WriteFile(loc.ConfigPath, []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
		retired, err := retireDir(registry, clone)
		if err != nil {
			t.Fatal(err)
		}
		if retired != "" {
			t.Errorf("a connected clone should not be retired, retired to %s", retired)
		}
		if err := os.WriteFile(loc.ConfigPath, []byte(otherRepo), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("a held clone lock refuses the retirement", func(t *testing.T) {
		unlock, err := registry.LockClones(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		_, err = retireDir(registry, clone)
		if err := unlock(); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(err, repos.ErrClonesBusy) {
			t.Errorf("a clone being connected should not be judged, got %v", err)
		}
	})

	t.Run("an empty repos list refuses the retirement", func(t *testing.T) {
		if err := os.WriteFile(loc.ConfigPath, []byte("repos: []\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := retireDir(registry, clone)
		if !errors.Is(err, repos.ErrClonePassRefused) {
			t.Errorf("a repos list emptied since the clone was listed should not retire it, got %v", err)
		}
		if _, err := os.Stat(clone); err != nil {
			t.Errorf("the clone should stay in place, stat says %v", err)
		}
		if err := os.WriteFile(loc.ConfigPath, []byte(otherRepo), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("a dropped, unconnected clone is moved out of the cache", func(t *testing.T) {
		retired, err := retireDir(registry, clone)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(clone); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the clone should be gone from its place, stat says %v", err)
		}
		if _, err := os.Stat(filepath.Join(retired, ".git")); err != nil {
			t.Errorf("the retired clone should await deletion, stat says %v", err)
		}
		if dropped, err := registry.Disconnected("github.com/acme/readded"); err != nil || dropped {
			t.Errorf("retiring the clone should spend its disconnection record, still recorded %v, %v", dropped, err)
		}
	})
}

func TestRetireUnconnectedCloneKeepsAClonesHoldingAConnectedOne(t *testing.T) {
	cases := []struct {
		name      string
		connected string
		nested    bool
		gitFile   bool
	}{
		{name: "a connected clone inside it", connected: "git.example.com/foo/bar", nested: true},
		{name: "a connected repo below its path, not cloned yet", connected: "git.example.com/foo/bar"},
		{name: "a connected worktree below it, its ID cased otherwise", connected: "git.example.com/FOO/bar", nested: true, gitFile: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
			config := "repos:\n  - repo_id: " + tc.connected + "\n    clone_url: git@git.example.com:foo/bar.git\n"
			if err := os.WriteFile(loc.ConfigPath, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			clone := filepath.Join(loc.CacheRoot, "git.example.com", "foo")
			inner := filepath.Join(clone, "bar", ".git")
			dirs := []string{filepath.Join(clone, ".git")}
			if tc.nested && !tc.gitFile {
				dirs = append(dirs, inner)
			}
			for _, dir := range dirs {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if tc.gitFile {
				if err := os.MkdirAll(filepath.Dir(inner), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(inner, []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			registry := repos.NewRegistry(loc)
			markDisconnected(t, registry, "git.example.com/foo")
			retired, err := retireDir(registry, clone)
			if !errors.Is(err, repos.ErrCloneHoldsClones) {
				t.Errorf("a clone holding a connected one should not be retired, got %q, %v", retired, err)
			}
			if _, err := os.Stat(filepath.Join(clone, ".git")); err != nil {
				t.Errorf("the clone should stay in place, stat says %v", err)
			}
			if tc.nested {
				if _, err := os.Stat(inner); err != nil {
					t.Errorf("the connected clone inside should stay, stat says %v", err)
				}
			}
		})
	}
}

func TestDisconnectionRecordLeftEmptyHoldsNothing(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	if err := os.WriteFile(loc.ConfigPath, []byte(otherRepo), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(loc.CacheRoot, "github.com", "acme", "dropped", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(loc.CacheRoot, ".disconnected", url.PathEscape("github.com/acme/dropped"))
	if err := os.MkdirAll(filepath.Dir(record), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	stale, err := repos.NewRegistry(loc).UnconnectedClones()
	if err != nil {
		t.Fatalf("an empty record should neither fail the clone pass nor count: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("an empty record should remove no clone, listed %v", stale)
	}
}

func TestDisconnectionRecordFollowsSymlinks(t *testing.T) {
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "cache")
	realDir := filepath.Join(root, "real")
	otherDir := filepath.Join(root, "other")
	link := filepath.Join(root, "link")
	for _, dir := range []string{realDir, otherDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(otherRepo), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	viaLink := repos.NewRegistry(repos.Locations{ConfigPath: filepath.Join(link, "config.yaml"), CacheRoot: cacheRoot, StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")})
	viaReal := repos.NewRegistry(repos.Locations{ConfigPath: filepath.Join(realDir, "config.yaml"), CacheRoot: cacheRoot, StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")})
	markDisconnected(t, viaLink, "github.com/acme/dropped")

	dropped, err := viaReal.Disconnected("github.com/acme/dropped")
	if err != nil {
		t.Fatal(err)
	}
	if !dropped {
		t.Error("a record written through a symlink should hold for the config it points to")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(otherDir, link); err != nil {
		t.Fatal(err)
	}
	dropped, err = viaLink.Disconnected("github.com/acme/dropped")
	if err != nil {
		t.Fatal(err)
	}
	if dropped {
		t.Error("a record should not hold for another config a retargeted symlink reaches")
	}
}

func TestRetireUnconnectedCloneOfADeepRepoID(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	if err := os.WriteFile(loc.ConfigPath, []byte(otherRepo), 0o600); err != nil {
		t.Fatal(err)
	}
	segments := []string{"git.example.com"}
	for range 9 {
		segments = append(segments, strings.Repeat("deep", 8))
	}
	repoID := model.RepoID(strings.Join(segments, "/"))
	clone := filepath.Join(append([]string{loc.CacheRoot}, segments...)...)
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	registry := repos.NewRegistry(loc)
	markDisconnected(t, registry, repoID)
	retired, err := retireDir(registry, clone)
	if err != nil {
		t.Fatalf("a repo ID too long for one file name should still retire its clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(retired, ".git")); err != nil {
		t.Errorf("the retired clone should await deletion, stat says %v", err)
	}
}

// retireDir retires the cache clone, or the retired clone, at dir.
func retireDir(r *repos.Registry, dir string) (string, error) {
	clone := repos.UnconnectedClone{Dir: dir}
	rel, err := filepath.Rel(r.CacheRoot(), dir)
	if err == nil && !strings.HasPrefix(rel, ".") {
		clone.RepoID = model.RepoID(filepath.ToSlash(rel))
	}
	return r.RetireUnconnectedClone(clone, nil)
}

func dirsOf(clones []repos.UnconnectedClone) []string {
	dirs := make([]string, 0, len(clones))
	for _, clone := range clones {
		dirs = append(dirs, clone.Dir)
	}
	return dirs
}
