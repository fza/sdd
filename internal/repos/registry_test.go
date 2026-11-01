package repos_test

import (
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
