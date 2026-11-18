package handlers_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/test"
)

// unstampedStore writes a store as an sdd that keeps no use stamps leaves it:
// a manifest naming a replaced and a current version, their rows, the store
// lock and the generation marker. It adds what this sdd's pruning would take
// from a store of its own: a row no version names and a staging folder, both
// older than a day.
func unstampedStore(t *testing.T, dir string, replacedAt time.Time) {
	t.Helper()
	err := index.WriteStore(t.Context(), dir, func(idx *index.Index) error {
		m := &index.Manifest{Version: 1, Entries: map[string]index.EntryState{}}
		for _, v := range []index.EntryVersion{
			{Hash: "old", ChunkIDs: []string{"e#v-old#summary"}, IndexedAt: replacedAt.Add(-time.Hour)},
			{Hash: "new", ChunkIDs: []string{"e#v-new#summary"}, IndexedAt: replacedAt},
		} {
			row := index.Row{EntryID: "e", EntryHash: v.Hash, ChunkID: v.ChunkIDs[0], Text: "t", Body: "t", ContentHash: index.HashContent("t"), ModelFingerprint: "m", IsSummary: true, Embedding: []float32{1, 0}}
			if err := idx.UpsertEntry(context.Background(), "e", nil, []index.Row{row}); err != nil {
				return err
			}
			m.AddVersion("e", v)
		}
		return m.Save(dir)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "used")); err != nil {
		t.Fatal(err)
	}
	rows, err := filepath.Glob(filepath.Join(dir, "chromem", "*", "*.gob"))
	if err != nil || len(rows) == 0 {
		t.Fatalf("finding the store's rows: %v, %v", rows, err)
	}
	orphan := filepath.Join(filepath.Dir(rows[0]), "ffffffff.gob")
	if err := os.WriteFile(orphan, []byte("row"), 0o644); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(dir, ".entry-123")
	if err := os.Mkdir(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-model.CacheLeftoverAge - time.Hour)
	for _, path := range []string{orphan, staging} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
}

// The index tree is shared with every sdd on the machine. A store holding no
// use stamp is one this sdd has no record of using: the run prunes no version
// of it, sweeps nothing in it and never removes it, whatever its age.
func TestCacheHandlerPruneLeavesStoresWithoutAUseStampAlone(t *testing.T) {
	f := newPruneFixture(t, "")
	long := time.Now().Add(-pruneRetention - time.Hour)
	repo := filepath.Join(f.loc.CacheRoot, "index", "github.com", "acme", "shared")
	inUse, unused, opened := filepath.Join(repo, "aaa"), filepath.Join(repo, "bbb"), filepath.Join(repo, "ccc")
	unstampedStore(t, inUse, long)
	unstampedStore(t, unused, long)
	if err := os.Chtimes(filepath.Join(unused, "manifest.json"), long, long); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(opened, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opened, ".lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(opened, long, long); err != nil {
		t.Fatal(err)
	}
	before := test.TreeSnapshot(t, repo)

	var counts model.CachePruneCounts
	err := f.handler(fixedHashes{}).Prune(t.Context(), &command.PruneCacheCmd{OnPruned: func(c model.CachePruneCounts) { counts = c }})
	if err != nil {
		t.Fatalf("pruning: %v", err)
	}

	if changes := test.TreeChanges(before, test.TreeSnapshot(t, repo)); len(changes) > 0 {
		t.Errorf("stores without a use stamp should stay byte for byte, the run left: %v", changes)
	}
	if counts.Versions != 1 || counts.Stores != 1 || counts.Leftovers != 0 {
		t.Errorf("the run should count only what it took from the fixture's stamped stores, got %+v", counts)
	}
}
