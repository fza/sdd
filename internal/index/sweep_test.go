package index_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"

	"github.com/networkteam/sdd/internal/index"
)

const leftoverAge = 24 * time.Hour

func TestSweepStore(t *testing.T) {
	now := time.Now()
	old := now.Add(-leftoverAge - time.Hour)
	named := version("a", index.DerivationCurrent, now, "e#v-a#summary")

	// orphans adds two rows the manifest does not name, one aged past
	// leftoverAge, and returns their paths.
	orphans := func(t *testing.T, namedRow string) (aged, young string) {
		t.Helper()
		chromem := filepath.Dir(namedRow)
		aged, young = filepath.Join(chromem, "deadbeef.gob"), filepath.Join(chromem, "feedface.gob")
		for _, path := range []string{aged, young} {
			if err := os.WriteFile(path, []byte("row"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		age(t, old, aged)
		return aged, young
	}

	t.Run("orphan rows and staging dirs older than a day go", func(t *testing.T) {
		dir := storeWithVersions(t, named)
		namedRow := rowFile(t, dir)
		agedRow, youngRow := orphans(t, namedRow)
		agedStage, youngStage := filepath.Join(dir, ".entry-1"), filepath.Join(dir, ".entry-2")
		for _, stage := range []string{agedStage, youngStage} {
			if err := os.MkdirAll(stage, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		age(t, old, agedStage)

		swept, err := index.SweepStore(dir, now, leftoverAge)
		if err != nil {
			t.Fatalf("sweeping: %v", err)
		}
		if swept.OrphanRows != 1 || swept.Temps != 1 || swept.Bytes == 0 {
			t.Errorf("the sweep should report one orphan row and one staging dir, got %+v", swept)
		}
		for path, kept := range map[string]bool{agedRow: false, youngRow: true, agedStage: false, youngStage: true, namedRow: true} {
			if _, err := os.Stat(path); (err == nil) != kept {
				t.Errorf("%s kept should be %v, stat says %v", filepath.Base(path), kept, err)
			}
		}
	})

	t.Run("a store a fill holds is skipped", func(t *testing.T) {
		dir := storeWithVersions(t, named)
		agedRow, _ := orphans(t, rowFile(t, dir))
		fill := flock.New(filepath.Join(dir, ".fill.lock"))
		if _, err := fill.TryLock(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = fill.Unlock() }()
		if _, err := index.SweepStore(dir, now, leftoverAge); !errors.Is(err, index.ErrLockBusy) {
			t.Errorf("sweeping a store a fill holds should report it busy, got %v", err)
		}
		if _, err := os.Stat(agedRow); err != nil {
			t.Errorf("a fill may be about to name the row, so it should stay: %v", err)
		}
	})
}

// rowFile is the file of the one row a store from storeWithVersions holds;
// call it before adding other rows.
func rowFile(t *testing.T, dir string) string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(filepath.Join(dir, "chromem"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() != "00000000.gob" {
			found = append(found, path)
		}
		return err
	})
	if err != nil || len(found) != 1 {
		t.Fatalf("finding the store's one row: %v, %v", found, err)
	}
	return found[0]
}

func TestRemoveRetiredStore(t *testing.T) {
	now := time.Now()
	retired := func(t *testing.T, at time.Time) string {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "abc123")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{".lock", ".fill.lock"} {
			if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		age(t, at, dir)
		return dir
	}

	t.Run("an emptied store older than a day goes with its locks", func(t *testing.T) {
		dir := retired(t, now.Add(-leftoverAge-time.Hour))
		removed, err := index.RemoveRetiredStore(dir, now, leftoverAge)
		if err != nil || !removed {
			t.Fatalf("removing should succeed, got %v, %v", removed, err)
		}
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the retired store should be gone, stat says %v", err)
		}
	})

	t.Run("a store emptied within a day stays", func(t *testing.T) {
		dir := retired(t, now.Add(-time.Hour))
		if removed, err := index.RemoveRetiredStore(dir, now, leftoverAge); err != nil || removed {
			t.Errorf("a young retired store should stay, got %v, %v", removed, err)
		}
	})

	t.Run("a store a fill wrote into again stays", func(t *testing.T) {
		dir := retired(t, now.Add(-leftoverAge-time.Hour))
		manifest := filepath.Join(dir, "manifest.json")
		if err := os.WriteFile(manifest, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		age(t, now.Add(-leftoverAge-time.Hour), dir)
		if removed, err := index.RemoveRetiredStore(dir, now, leftoverAge); err != nil || removed {
			t.Errorf("a store holding more than its locks should stay, got %v, %v", removed, err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".lock")); err != nil {
			t.Errorf("a store holding more than its locks should keep them: %v", err)
		}
	})

	t.Run("a store whose lock is held stays", func(t *testing.T) {
		dir := retired(t, now.Add(-leftoverAge-time.Hour))
		held := flock.New(filepath.Join(dir, ".lock"))
		if _, err := held.TryRLock(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = held.Unlock() }()
		if _, err := index.RemoveRetiredStore(dir, now, leftoverAge); !errors.Is(err, index.ErrLockBusy) {
			t.Errorf("a held retired store should report busy, got %v", err)
		}
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("a held retired store should stay: %v", err)
		}
	})
}
