package repos_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"

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
	mkdirs(t, old, oldAdding, oldCloning, filepath.Join(clone, ".git"))
	mkdirs(t, young, youngAdding, youngCloning)

	removed, err := repos.NewRegistry(loc).SweepCloneStaging(now, minAge)
	if err != nil || removed != 2 {
		t.Fatalf("the two staging dirs older than a day should go, removed %d: %v", removed, err)
	}
	for dir, wantKept := range map[string]bool{oldAdding: false, oldCloning: false, youngAdding: true, youngCloning: true, clone: true} {
		if _, err := os.Stat(dir); (err == nil) != wantKept {
			t.Errorf("%s kept should be %v, stat says %v", dir, wantKept, err)
		}
	}
}

func TestSweepLegacyHostDirs(t *testing.T) {
	root := t.TempDir()
	loc := repos.Locations{CacheRoot: filepath.Join(root, "cache"), DataRoot: filepath.Join(root, "data")}
	now := time.Now()
	old := now.Add(-minAge - time.Hour)
	host := func(name string) string { return filepath.Join(loc.CacheRoot, name) }
	lockIn := func(dir string) string {
		path := filepath.Join(dir, "acme", "r", ".sdd", "graph", ".sdd-runtime", "graph.lock")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	ageTree := func(dir string) {
		err := filepath.Walk(dir, func(path string, _ os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			return os.Chtimes(path, old, old)
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	empty := host("github.com")
	lockIn(empty)
	held := host("gitlab.com")
	heldLock := flock.New(lockIn(held))
	if _, err := heldLock.TryLock(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = heldLock.Unlock() }()
	withFile := host("codeberg.org")
	lockIn(withFile)
	if err := os.WriteFile(filepath.Join(withFile, "note.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	withClone := host("example.com")
	mkdirs(t, old, filepath.Join(withClone, "acme", "r", ".git"))
	young := host("example.org")
	lockIn(young)
	for _, dir := range []string{empty, held, withFile, withClone} {
		ageTree(dir)
	}
	mkdirs(t, old, filepath.Join(loc.CacheRoot, "index"), filepath.Join(loc.CacheRoot, "graph"))

	removed, err := repos.NewRegistry(loc).SweepLegacyHostDirs(now, minAge)
	if err != nil || removed != 1 {
		t.Fatalf("only the host folder holding nothing but a free lock should go, removed %d: %v", removed, err)
	}
	for dir, wantKept := range map[string]bool{
		empty: false, held: true, withFile: true, withClone: true, young: true,
		filepath.Join(loc.CacheRoot, "index"): true, filepath.Join(loc.CacheRoot, "graph"): true,
	} {
		if _, err := os.Stat(dir); (err == nil) != wantKept {
			t.Errorf("%s kept should be %v, stat says %v", filepath.Base(dir), wantKept, err)
		}
	}
}
