package index_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/gofrs/flock"

	"github.com/networkteam/sdd/internal/index"
)

const retention = 90 * 24 * time.Hour

func version(hash, derivation string, at time.Time, chunks ...string) index.EntryVersion {
	return index.EntryVersion{Hash: hash, Derivation: derivation, ChunkIDs: chunks, IndexedAt: at}
}

func TestPruneVersions(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	long := now.Add(-retention - time.Hour)
	recent := now.Add(-time.Hour)
	older := long.Add(-time.Hour)
	const cur = index.DerivationCurrent

	cases := []struct {
		name     string
		entries  map[string]index.EntryState
		uses     map[string]time.Time
		keep     map[string]string
		limit    int
		dropped  []string
		versions int
	}{
		{
			name: "a version replaced longer ago than retention goes",
			entries: map[string]index.EntryState{"e": {Versions: []index.EntryVersion{
				version("a", cur, older, "e#a"), version("b", cur, long, "e#b"),
			}}},
			dropped: []string{"e#a"}, versions: 1,
		},
		{
			name: "a version replaced recently stays",
			entries: map[string]index.EntryState{"e": {Versions: []index.EntryVersion{
				version("a", cur, older, "e#a"), version("b", cur, recent, "e#b"),
			}}},
		},
		{
			name: "the newest version stays however old",
			entries: map[string]index.EntryState{"e": {Versions: []index.EntryVersion{
				version("a", cur, older, "e#a"),
			}}},
		},
		{
			name: "replacement is counted from the next newer version",
			entries: map[string]index.EntryState{"e": {Versions: []index.EntryVersion{
				version("a", cur, older, "e#a"), version("b", cur, recent, "e#b"), version("c", cur, now, "e#c"),
			}}},
		},
		{
			name: "a version current for the checkout stays",
			entries: map[string]index.EntryState{"e": {Versions: []index.EntryVersion{
				version("a", cur, older, "e#a"), version("b", cur, long, "e#b"),
			}}},
			keep: map[string]string{"e": "a"},
		},
		{
			name: "another rule's versions go once its newest write is past retention",
			entries: map[string]index.EntryState{
				"e": {Versions: []index.EntryVersion{version("a", "", long, "e#v-abcdef01#summary"), version("b", cur, older, "e#b")}},
				"f": {Versions: []index.EntryVersion{version("c", "", older, "f#v-abcdef01#summary")}},
			},
			dropped: []string{"e#v-abcdef01#summary", "f#v-abcdef01#summary"}, versions: 2,
		},
		{
			name: "another rule's versions stay while a binary of that rule stamped the store",
			entries: map[string]index.EntryState{"e": {Versions: []index.EntryVersion{
				version("a", "", long, "e#v-abcdef01#summary"), version("b", cur, older, "e#b"),
			}}},
			uses: map[string]time.Time{index.GroupPreDerivation: recent},
		},
		{
			name: "another rule's versions stay while one of them was written recently",
			entries: map[string]index.EntryState{
				"e": {Versions: []index.EntryVersion{version("a", "", long, "e#v-abcdef01#summary")}},
				"f": {Versions: []index.EntryVersion{version("c", "", recent, "f#v-abcdef01#summary")}},
			},
		},
		{
			name: "the limit caps the versions removed",
			entries: map[string]index.EntryState{
				"e": {Versions: []index.EntryVersion{version("a", cur, older, "e#a"), version("b", cur, long, "e#b")}},
				"f": {Versions: []index.EntryVersion{version("c", cur, older, "f#c"), version("d", cur, long, "f#d")}},
			},
			limit: 1, dropped: []string{"e#a"}, versions: 1,
		},
		{
			name: "a chunk a kept version still names is not deleted",
			entries: map[string]index.EntryState{"e": {Versions: []index.EntryVersion{
				version("a", cur, older, "e#shared", "e#a"), version("b", cur, long, "e#shared"),
			}}},
			dropped: []string{"e#a"}, versions: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &index.Manifest{Version: 1, Entries: tc.entries}
			dropped, versions := m.PruneVersions(now, retention, tc.uses, tc.keep, tc.limit)
			if !slices.Equal(dropped, tc.dropped) || versions != tc.versions {
				t.Errorf("pruned %v (%d versions), want %v (%d versions)", dropped, versions, tc.dropped, tc.versions)
			}
			for _, state := range m.Entries {
				for _, v := range state.Versions {
					for _, id := range v.ChunkIDs {
						if slices.Contains(tc.dropped, id) {
							t.Errorf("the manifest should no longer name a removed version, still holds %s", id)
						}
					}
				}
			}
		})
	}
}

// storeWithVersions writes rows for the given versions of entry e and a
// manifest naming them.
func storeWithVersions(t *testing.T, versions ...index.EntryVersion) string {
	t.Helper()
	dir := t.TempDir()
	err := index.WriteStore(t.Context(), dir, func(idx *index.Index) error {
		m := &index.Manifest{Version: 1, Entries: map[string]index.EntryState{}}
		for _, v := range versions {
			row := fillRow("e", []float32{1, 0, 0, 0})
			row.ChunkID, row.EntryHash = v.ChunkIDs[0], v.Hash
			if err := idx.UpsertEntry(context.Background(), "e", nil, []index.Row{row}); err != nil {
				return err
			}
			m.AddVersion("e", v)
		}
		return m.Save(dir)
	})
	if err != nil {
		t.Fatalf("writing the store: %v", err)
	}
	return dir
}

func TestPruneStoreRemovesExpiredRows(t *testing.T) {
	now := time.Now()
	old := version("a", index.DerivationCurrent, now.Add(-retention-2*time.Hour), "e#v-a#summary")
	current := version("b", index.DerivationCurrent, now.Add(-retention-time.Hour), "e#v-b#summary")
	dir := storeWithVersions(t, old, current)

	pruned, err := index.PruneStore(t.Context(), dir, now, retention, nil)
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}
	if pruned.Versions != 1 || pruned.Bytes == 0 {
		t.Errorf("pruning should report the one replaced version and its bytes, got %+v", pruned)
	}
	if index.DocumentsSize(dir, old.ChunkIDs) != 0 {
		t.Error("the replaced version's row should be deleted")
	}
	if index.DocumentsSize(dir, current.ChunkIDs) == 0 {
		t.Error("the current version's row should stay")
	}
	m, err := index.LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(m.Entries["e"].Versions); got != 1 {
		t.Errorf("the manifest should keep only the current version, holds %d", got)
	}
}

func TestPruneStoreSkipsALockedStore(t *testing.T) {
	now := time.Now()
	dir := storeWithVersions(t,
		version("a", index.DerivationCurrent, now.Add(-retention-2*time.Hour), "e#v-a#summary"),
		version("b", index.DerivationCurrent, now.Add(-retention-time.Hour), "e#v-b#summary"))

	err := index.ReadStore(t.Context(), dir, func(*index.Index) error {
		pruned, err := index.PruneStore(t.Context(), dir, now, retention, nil)
		if !errors.Is(err, index.ErrLockBusy) {
			t.Errorf("pruning a store another process reads should report it busy, got %v", err)
		}
		if pruned.Versions != 0 {
			t.Errorf("pruning a busy store should remove nothing, removed %d", pruned.Versions)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func age(t *testing.T, at time.Time, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStoreUse(t *testing.T) {
	now := time.Now()
	long := now.Add(-retention - time.Hour)

	t.Run("a read stamps the store", func(t *testing.T) {
		dir := storeWithVersions(t, version("a", index.DerivationCurrent, long, "e#v-a#summary"))
		uses, err := index.RuleLastUses(dir)
		if err != nil {
			t.Fatal(err)
		}
		if now.Sub(uses[index.DerivationCurrent]) > time.Minute {
			t.Errorf("writing the store should stamp this binary's rule, stamps %v", uses)
		}
	})

	t.Run("an unused store is removed", func(t *testing.T) {
		dir := storeWithVersions(t, version("a", index.DerivationCurrent, long, "e#v-a#summary"))
		age(t, long, filepath.Join(dir, "used", index.DerivationCurrent+"@1"), filepath.Join(dir, "manifest.json"))
		freed, err := index.RemoveStore(dir, now, retention)
		if err != nil {
			t.Fatalf("removing: %v", err)
		}
		if freed == 0 {
			t.Error("removing a store should report the bytes freed")
		}
		for _, part := range []string{"manifest.json", "chromem", "used"} {
			if _, err := os.Stat(filepath.Join(dir, part)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the unused store's %s should be gone, stat says %v", part, err)
			}
		}
	})

	t.Run("a reader waiting on a store being removed finds an empty one", func(t *testing.T) {
		dir := storeWithVersions(t, version("a", index.DerivationCurrent, long, "e#v-a#summary"))
		age(t, long, filepath.Join(dir, "used", index.DerivationCurrent+"@1"), filepath.Join(dir, "manifest.json"))
		waiting := flock.New(filepath.Join(dir, ".lock"))
		var before []os.FileInfo
		for _, name := range []string{".lock", ".fill.lock"} {
			file, err := os.OpenFile(filepath.Join(dir, name), os.O_RDONLY|os.O_CREATE, 0o644)
			if err != nil {
				t.Fatal(err)
			}
			info, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			before = append(before, info)
		}
		if _, err := index.RemoveStore(dir, now, retention); err != nil {
			t.Fatalf("removing: %v", err)
		}
		for i, name := range []string{".lock", ".fill.lock"} {
			after, err := os.Stat(filepath.Join(dir, name))
			if err != nil || !os.SameFile(before[i], after) {
				t.Errorf("%s should stay the same file, so a process holding it open and one arriving later lock the same file", name)
			}
		}
		if _, err := waiting.TryRLock(); err != nil {
			t.Fatalf("a lock path opened before the removal should still lock: %v", err)
		}
		if err := waiting.Unlock(); err != nil {
			t.Fatal(err)
		}
		snapshot, err := index.OpenSearchSnapshot(t.Context(), dir, time.Second)
		if err != nil {
			t.Fatalf("searching the removed store should start an empty one: %v", err)
		}
		if snapshot.Count() != 0 {
			t.Error("a removed store should read as empty")
		}
		if err := snapshot.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("a store only read stays", func(t *testing.T) {
		dir := storeWithVersions(t, version("a", index.DerivationCurrent, long, "e#v-a#summary"))
		stamp := filepath.Join(dir, "used", index.DerivationCurrent+"@1")
		age(t, long, stamp, filepath.Join(dir, "manifest.json"))
		if _, err := index.OpenSearchSnapshot(t.Context(), dir, time.Second); err != nil {
			t.Fatalf("searching: %v", err)
		}
		freed, err := index.RemoveStore(dir, now, retention)
		if err != nil {
			t.Fatalf("removing: %v", err)
		}
		if freed != 0 {
			t.Error("a store a search used should stay")
		}
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("a store a search used should stay, stat says %v", err)
		}
	})

	t.Run("a store without stamps falls back to its manifest's last write", func(t *testing.T) {
		dir := storeWithVersions(t, version("a", index.DerivationCurrent, long, "e#v-a#summary"))
		if err := os.RemoveAll(filepath.Join(dir, "used")); err != nil {
			t.Fatal(err)
		}
		last, err := index.StoreLastUse(dir)
		if err != nil {
			t.Fatal(err)
		}
		if now.Sub(last) > time.Minute {
			t.Errorf("an unstamped store's last use should be its manifest write, got %v", last)
		}
	})
}

func TestStores(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "index", "github.com", "o", "r", "abc123")
	leftover := filepath.Join(root, "index", "github.com", "o", "r", ".def456.pruned")
	for _, dir := range []string{filepath.Join(store, "chromem"), leftover} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stores, leftovers, err := index.Stores(root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(stores, []string{store}) || !slices.Equal(leftovers, []string{leftover}) {
		t.Errorf("listing should find the store and the interrupted removal, got %v and %v", stores, leftovers)
	}
	if stores, _, err := index.Stores(t.TempDir()); err != nil || len(stores) != 0 {
		t.Errorf("a cache without an index tree should list nothing, got %v, %v", stores, err)
	}
}
