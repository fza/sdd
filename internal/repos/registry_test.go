package repos_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

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
	for _, dir := range []string{
		filepath.Join(loc.CacheRoot, "github.com", "acme", "kept", ".git"),
		filepath.Join(dropped, ".git"),
		filepath.Join(loc.CacheRoot, "index", "github.com", "acme", "x", ".git"),
		filepath.Join(loc.CacheRoot, ".adding-123", "clone", ".git"),
		filepath.Join(loc.CacheRoot, "github.com", "acme", "nogit"),
		filepath.Join(owned, "github.com", "acme", "kept", ".git"),
		filepath.Join(ownedDropped, ".git"),
		filepath.Join(owned, "gitlab.example.org", "team", "new.cloning", ".git"),
		filepath.Join(owned, ".adding-456", "clone", ".git"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	stale, err := repos.NewRegistry(loc).UnconnectedClones()
	if err != nil {
		t.Fatal(err)
	}
	want := []repos.UnconnectedClone{
		{Dir: ownedDropped, RepoID: "gitlab.example.org/team/graph", Owned: true},
		{Dir: dropped, RepoID: "gitlab.example.org/team/dropped"},
	}
	if !slices.Equal(stale, want) {
		t.Errorf("only the clones no connection names should be listed, the sdd-owned ones marked, got %+v", stale)
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
	if err := os.WriteFile(loc.ConfigPath, []byte("repos: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cached := repos.UnconnectedClone{Dir: filepath.Join(loc.CacheRoot, "github.com", "acme", "readded"), RepoID: "github.com/acme/readded"}
	owned := repos.UnconnectedClone{Dir: filepath.Join(loc.DataRoot, "repos", "github.com", "acme", "graph"), RepoID: "github.com/acme/graph", Owned: true}
	for _, clone := range []repos.UnconnectedClone{cached, owned} {
		if err := os.MkdirAll(filepath.Join(clone.Dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	registry := repos.NewRegistry(loc)
	letGo := func(string) (func(), error) { return func() {}, nil }

	t.Run("a clone connected since it was listed stays", func(t *testing.T) {
		config := "repos:\n  - repo_id: github.com/acme/readded\n    clone_url: git@github.com:acme/readded.git\n"
		if err := os.WriteFile(loc.ConfigPath, []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
		retired, err := registry.RetireUnconnectedClone(cached, letGo)
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
		_, err = registry.RetireUnconnectedClone(cached, letGo)
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

	t.Run("an unconnected cache clone is moved out of the cache", func(t *testing.T) {
		retired, err := registry.RetireUnconnectedClone(cached, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(cached.Dir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the clone should be gone from its place, stat says %v", err)
		}
		if _, err := os.Stat(filepath.Join(retired, ".git")); err != nil {
			t.Errorf("the retired clone should await deletion, stat says %v", err)
		}
	})
}
