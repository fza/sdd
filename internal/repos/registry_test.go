package repos_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

func TestUnconnectedClones(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	config := "repos:\n  - repo_id: github.com/acme/kept\n    clone_url: git@github.com:acme/kept.git\n"
	if err := os.WriteFile(loc.ConfigPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(loc.DataRoot, "repos")
	dropped := filepath.Join(loc.CacheRoot, "gitlab.example.org", "team", "dropped")
	ownedDropped := filepath.Join(owned, "gitlab.example.org", "team", "graph")
	ownedRetired := filepath.Join(owned, "gitlab.example.org", "team", ".old.pruned")
	for _, dir := range []string{
		filepath.Join(loc.CacheRoot, "github.com", "acme", "kept", ".git"),
		filepath.Join(dropped, ".git"),
		filepath.Join(loc.CacheRoot, "index", "github.com", "acme", "x", ".git"),
		filepath.Join(loc.CacheRoot, ".adding-123", "clone", ".git"),
		filepath.Join(loc.CacheRoot, "github.com", "acme", "nogit"),
		filepath.Join(owned, "github.com", "acme", "kept", ".git"),
		filepath.Join(ownedDropped, ".git"),
		filepath.Join(ownedRetired, ".git"),
		filepath.Join(owned, "gitlab.example.org", "team", "unlisted", ".git"),
		filepath.Join(owned, "gitlab.example.org", "team", "new.cloning", ".git"),
		filepath.Join(owned, ".adding-456", "clone", ".git"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	registry := repos.NewRegistry(loc)
	for _, repoID := range []model.RepoID{"gitlab.example.org/team/graph", "gitlab.example.org/team/dropped"} {
		if err := repos.NewManager(registry, nil).MarkDisconnected(repoID); err != nil {
			t.Fatal(err)
		}
	}
	stale, err := registry.UnconnectedClones()
	if err != nil {
		t.Fatal(err)
	}
	want := []repos.UnconnectedClone{{Dir: ownedDropped, RepoID: "gitlab.example.org/team/graph"}}
	if !slices.Equal(stale, want) {
		t.Errorf("only disconnected sdd-owned clones the repos list does not name should be listed, none under the cache root, got %+v", stale)
	}
}

func TestUnconnectedClonesRefuseAnUnsureRepoList(t *testing.T) {
	empty := "repos: []\n"
	cases := []struct {
		name    string
		config  *string
		dropped bool
		refused bool
	}{
		{name: "a missing config file with a disconnected clone", dropped: true, refused: true},
		{name: "an empty repos list with a disconnected clone", config: &empty, dropped: true, refused: true},
		{name: "a missing config file with no clone to remove"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), DataRoot: filepath.Join(root, "data")}
			if tc.config != nil {
				if err := os.WriteFile(loc.ConfigPath, []byte(*tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Join(loc.DataRoot, "repos", "github.com", "acme", "graph", ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			registry := repos.NewRegistry(loc)
			if tc.dropped {
				if err := repos.NewManager(registry, nil).MarkDisconnected("github.com/acme/graph"); err != nil {
					t.Fatal(err)
				}
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

func TestUnconnectedClonesNeedTheDataRoot(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache")}
	if _, err := repos.NewRegistry(loc).UnconnectedClones(); err == nil {
		t.Error("listing without a data root should fail rather than walk a relative path")
	}
}

func TestRetireUnconnectedClone(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), DataRoot: filepath.Join(root, "data")}
	if err := os.WriteFile(loc.ConfigPath, []byte(keptRepo), 0o600); err != nil {
		t.Fatal(err)
	}
	cached := repos.UnconnectedClone{Dir: filepath.Join(loc.CacheRoot, "github.com", "acme", "cached"), RepoID: "github.com/acme/cached"}
	readded := repos.UnconnectedClone{Dir: filepath.Join(loc.DataRoot, "repos", "github.com", "acme", "readded"), RepoID: "github.com/acme/readded"}
	owned := repos.UnconnectedClone{Dir: filepath.Join(loc.DataRoot, "repos", "github.com", "acme", "graph"), RepoID: "github.com/acme/graph"}
	registry := repos.NewRegistry(loc)
	for _, clone := range []repos.UnconnectedClone{cached, readded, owned} {
		if err := os.MkdirAll(filepath.Join(clone.Dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := repos.NewManager(registry, nil).MarkDisconnected(clone.RepoID); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("a clone under the cache root is refused and stays", func(t *testing.T) {
		retired, err := registry.RetireUnconnectedClone(cached, letGo)
		if err == nil {
			t.Errorf("a clone outside the sdd-owned clones should be refused, retired to %q", retired)
		}
		if _, err := os.Stat(filepath.Join(cached.Dir, ".git")); err != nil {
			t.Errorf("the clone under the cache root should stay in place, stat says %v", err)
		}
	})

	t.Run("a clone connected since it was listed stays", func(t *testing.T) {
		config := "repos:\n  - repo_id: github.com/acme/readded\n    clone_url: git@github.com:acme/readded.git\n"
		if err := os.WriteFile(loc.ConfigPath, []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
		retired, err := registry.RetireUnconnectedClone(readded, letGo)
		if err != nil {
			t.Fatal(err)
		}
		if retired != "" {
			t.Errorf("a connected clone should not be retired, retired to %s", retired)
		}
		if err := os.WriteFile(loc.ConfigPath, []byte(keptRepo), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("a held clone lock refuses the retirement", func(t *testing.T) {
		unlock, err := registry.LockClones(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		_, err = registry.RetireUnconnectedClone(readded, letGo)
		if err := unlock(); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(err, repos.ErrClonesBusy) {
			t.Errorf("a clone being connected should not be judged, got %v", err)
		}
	})

	t.Run("an sdd-owned clone its hold keeps stays", func(t *testing.T) {
		keep := func(string) (func(), error) { return nil, fmt.Errorf("%w: a sync holds it", repos.ErrCloneInUse) }
		_, err := registry.RetireUnconnectedClone(owned, keep)
		if !errors.Is(err, repos.ErrCloneInUse) {
			t.Errorf("the hold's verdict should be returned, got %v", err)
		}
		if _, err := os.Stat(owned.Dir); err != nil {
			t.Errorf("the clone should stay in place, stat says %v", err)
		}
	})

	t.Run("an sdd-owned clone is retired under its hold", func(t *testing.T) {
		var heldWhileRenamed, released bool
		hold := func(dir string) (func(), error) {
			return func() {
				_, err := os.Stat(dir)
				heldWhileRenamed = errors.Is(err, os.ErrNotExist)
				released = true
			}, nil
		}
		retired, err := registry.RetireUnconnectedClone(owned, hold)
		if err != nil {
			t.Fatal(err)
		}
		if !released || !heldWhileRenamed {
			t.Errorf("the hold should be released after the clone left its place, released %v, renamed first %v", released, heldWhileRenamed)
		}
		if _, err := os.Stat(filepath.Join(retired, ".git")); err != nil {
			t.Errorf("the retired clone should await deletion, stat says %v", err)
		}
	})
}

// The state sweep refuses a state root from another source than the cache
// and data roots; the config root's source does not count.
func TestDefaultLocationsNameAStateRootFromAnotherSource(t *testing.T) {
	cases := []struct {
		name  string
		set   []string
		mixed bool
	}{
		{name: "state from the home default, cache and data redirected", set: []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME"}, mixed: true},
		{name: "state redirected, cache and data from the home default", set: []string{"XDG_STATE_HOME"}, mixed: true},
		{name: "only the config root redirected", set: []string{"XDG_CONFIG_HOME"}},
		{name: "all four redirected", set: []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"}},
		{name: "none redirected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
				t.Setenv(name, "")
			}
			for _, name := range tc.set {
				t.Setenv(name, filepath.Join(root, name))
			}
			loc, err := repos.DefaultLocations()
			if err != nil {
				t.Fatal(err)
			}
			if mixed := loc.MixedStateXDG != ""; mixed != tc.mixed {
				t.Errorf("the state root should count as from another source %v, MixedStateXDG %q", tc.mixed, loc.MixedStateXDG)
			}
		})
	}
}

// The XDG roots belong together only when one source set them all: a process
// overriding some of them reaches the clones of another repos list.
func TestUnconnectedClonesRefuseMixedXDGRoots(t *testing.T) {
	cases := []struct {
		name    string
		unset   []string
		refused bool
	}{
		{name: "all four roots from the environment"},
		{name: "the data root from the home default", unset: []string{"XDG_DATA_HOME"}, refused: true},
		{name: "the config root from the home default", unset: []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME"}, refused: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
				t.Setenv(name, filepath.Join(root, name))
			}
			for _, name := range tc.unset {
				t.Setenv(name, "")
			}
			loc, err := repos.DefaultLocations()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(loc.ConfigPath), 0o755); err != nil {
				t.Fatal(err)
			}
			config := "repos:\n  - repo_id: github.com/acme/kept\n    clone_url: git@github.com:acme/kept.git\n"
			if err := os.WriteFile(loc.ConfigPath, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(loc.DataRoot, "repos", "github.com", "acme", "dropped", ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			registry := repos.NewRegistry(loc)
			if err := repos.NewManager(registry, nil).MarkDisconnected("github.com/acme/dropped"); err != nil {
				t.Fatal(err)
			}
			_, err = registry.UnconnectedClones()
			if refused := errors.Is(err, repos.ErrClonePassRefused); refused != tc.refused {
				t.Errorf("the clone pass should refuse roots from mixed sources only, refused = %v, err %v", refused, err)
			}
			if !tc.refused && err != nil {
				t.Errorf("listing should succeed, got %v", err)
			}
		})
	}
}
