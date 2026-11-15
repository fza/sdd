package repos_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/repos"
)

func TestUnconnectedClones(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(root, "config.yaml"), CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state")}
	config := "repos:\n  - repo_id: github.com/acme/kept\n    clone_url: git@github.com:acme/kept.git\n"
	if err := os.WriteFile(loc.ConfigPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(loc.CacheRoot, "github.com", "acme", "kept")
	dropped := filepath.Join(loc.CacheRoot, "gitlab.example.org", "team", "dropped")
	for _, dir := range []string{
		filepath.Join(kept, ".git"),
		filepath.Join(dropped, ".git"),
		filepath.Join(loc.CacheRoot, "index", "github.com", "acme", "x", ".git"),
		filepath.Join(loc.CacheRoot, ".adding-123", "clone", ".git"),
		filepath.Join(loc.CacheRoot, "github.com", "acme", "nogit"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	stale, err := repos.NewRegistry(loc).UnconnectedClones()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(stale, []string{dropped}) {
		t.Errorf("only the clone no connection names should be listed, got %v", stale)
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

	t.Run("an unconnected clone is moved out of the cache", func(t *testing.T) {
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
	})
}
