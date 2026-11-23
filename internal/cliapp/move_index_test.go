package cliapp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/repos"
)

func machineLocations(t *testing.T) repos.Locations {
	t.Helper()
	locations, err := repos.DefaultLocations()
	if err != nil {
		t.Fatal(err)
	}
	return locations
}

func stableRoot(t *testing.T, dir string) string {
	t.Helper()
	root, err := git.StableRepoRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// The search index store stays where it is; the repository key the moved
// graph is read under links to it, and the move back leaves the original
// store a directory of its own.
func TestMoveLinksTheSearchIndexStore(t *testing.T) {
	repo := graphRepoToMove(t)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(repo, "xdg-cache"))
	project := filepath.Join(filepath.Dir(repo), "graph-home")
	cacheRoot := machineLocations(t).CacheRoot
	oldKey := index.RepoKey("", stableRoot(t, repo))
	store := index.StoreDir(cacheRoot, oldKey, "embedder")
	writeFile(t, filepath.Join(store, "manifest.json"), "{}\n")

	runApp(t, repo, "move", "--project-dir", "../graph-home")

	newKey := index.RepoKey("", stableRoot(t, project))
	linked := index.StoreDir(cacheRoot, newKey, "embedder")
	if index.RealStoreDir(linked) != index.RealStoreDir(store) {
		t.Fatal("the new repository key should reach the store the graph had")
	}
	keyDir := filepath.Join(cacheRoot, "index", filepath.FromSlash(newKey))
	if target, err := os.Readlink(keyDir); err != nil || filepath.IsAbs(target) {
		t.Errorf("the new key should be a relative link, got %q: %v", target, err)
	}

	runApp(t, repo, "move", "--graph-dir", ".sdd/graph")
	info, err := os.Lstat(filepath.Join(cacheRoot, "index", filepath.FromSlash(oldKey)))
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("the original store should stay a real directory: %v", err)
	}
	if manifest := readText(t, filepath.Join(store, "manifest.json")); manifest != "{}\n" {
		t.Errorf("the store should keep its content, got %q", manifest)
	}
}
