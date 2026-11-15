package repos_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

func markDisconnected(t *testing.T, registry *repos.Registry, repoID model.RepoID) {
	t.Helper()
	if err := repos.NewManager(registry, nil).MarkDisconnected(repoID); err != nil {
		t.Fatal(err)
	}
}

func TestUnconnectedClones(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state")}
	config := "repos:\n  - repo_id: github.com/acme/kept\n    clone_url: git@github.com:acme/kept.git\n"
	if err := os.WriteFile(loc.ConfigPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(loc.CacheRoot, "github.com", "acme", "kept")
	dropped := filepath.Join(loc.CacheRoot, "gitlab.example.org", "team", "dropped")
	retired := filepath.Join(loc.CacheRoot, "gitlab.example.org", "team", ".old.pruned")
	for _, dir := range []string{
		filepath.Join(kept, ".git"),
		filepath.Join(dropped, ".git"),
		filepath.Join(retired, ".git"),
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
	if !slices.Equal(stale, []string{retired, dropped}) {
		t.Errorf("only clones repo remove dropped that no connection names, and retired leftovers, should be listed, got %v", stale)
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
			loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"), MixedXDG: tc.mixed}
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
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state")}
	if err := os.WriteFile(loc.ConfigPath, []byte("repos: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(loc.CacheRoot, "github.com", "acme", "readded")
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	registry := repos.NewRegistry(loc)

	t.Run("a clone no repo remove dropped stays", func(t *testing.T) {
		retired, err := registry.RetireUnconnectedClone(clone)
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
		retired, err := registry.RetireUnconnectedClone(clone)
		if err != nil {
			t.Fatal(err)
		}
		if retired != "" {
			t.Errorf("a connected clone should not be retired, retired to %s", retired)
		}
		if err := os.WriteFile(loc.ConfigPath, []byte("repos: []\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("a held clone lock refuses the retirement", func(t *testing.T) {
		unlock, err := registry.LockClones(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		_, err = registry.RetireUnconnectedClone(clone)
		if err := unlock(); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(err, repos.ErrClonesBusy) {
			t.Errorf("a clone being connected should not be judged, got %v", err)
		}
	})

	t.Run("a dropped, unconnected clone is moved out of the cache", func(t *testing.T) {
		retired, err := registry.RetireUnconnectedClone(clone)
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
