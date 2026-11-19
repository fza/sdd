package repos_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/repos"
)

const minAge = 24 * time.Hour

func mkdirs(t *testing.T, at time.Time, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(dir, at, at); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSweepCloneStaging(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{CacheRoot: filepath.Join(root, "cache"), DataRoot: filepath.Join(root, "data")}
	repoRoot := filepath.Join(loc.DataRoot, "repos")
	now := time.Now()
	old, young := now.Add(-minAge-time.Hour), now.Add(-time.Hour)
	oldAdding, youngAdding := filepath.Join(repoRoot, ".adding-1"), filepath.Join(repoRoot, ".adding-2")
	oldCloning := filepath.Join(repoRoot, "github.com", "acme", "x.cloning")
	youngCloning := filepath.Join(repoRoot, "github.com", "acme", "y.cloning")
	clone := filepath.Join(repoRoot, "github.com", "acme", "z")
	nestedAdding := filepath.Join(repoRoot, "github.com", ".adding-3")
	inClone := filepath.Join(clone, "docs.cloning")
	mkdirs(t, old, oldAdding, oldCloning, filepath.Join(clone, ".git"), nestedAdding, inClone)
	mkdirs(t, young, youngAdding, youngCloning)

	removed, err := repos.NewRegistry(loc).SweepCloneStaging(now, minAge)
	if err != nil || removed != 2 {
		t.Fatalf("the two staging dirs older than a day should go, removed %d: %v", removed, err)
	}
	for dir, wantKept := range map[string]bool{oldAdding: false, oldCloning: false, youngAdding: true, youngCloning: true, clone: true, nestedAdding: true, inClone: true} {
		if _, err := os.Stat(dir); (err == nil) != wantKept {
			t.Errorf("%s kept should be %v, stat says %v", dir, wantKept, err)
		}
	}
}
