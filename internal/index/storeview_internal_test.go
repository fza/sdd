package index

import (
	"testing"
	"time"
)

func publishForView(t *testing.T, indexDir string, entryIDs ...string) {
	t.Helper()
	err := WriteStore(t.Context(), indexDir, func(idx *Index) error {
		manifest, err := LoadManifest(indexDir)
		if err != nil {
			return err
		}
		for i, entryID := range entryIDs {
			row := Row{EntryID: entryID, EntryHash: "h-" + entryID, ChunkID: entryID + "#summary", Text: entryID, Body: entryID, ModelFingerprint: "test/model/4", IsSummary: true, Embedding: []float32{float32(i + 1), 1, 0, 0}}
			if err := idx.UpsertEntry(t.Context(), entryID, nil, []Row{row}); err != nil {
				return err
			}
			manifest.AddVersion(entryID, EntryVersion{Hash: row.EntryHash, Fingerprint: row.ModelFingerprint, ChunkIDs: []string{row.ChunkID}})
		}
		return manifest.Save(indexDir)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := RefreshSegments(t.Context(), indexDir); err != nil {
		t.Fatal(err)
	}
}

func searchOnce(t *testing.T, indexDir string) {
	t.Helper()
	snapshot, err := OpenSearchSnapshot(t.Context(), indexDir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
}

func mappedSegments(indexDir string) []string {
	view := viewFor(indexDir)
	view.mu.Lock()
	defer view.mu.Unlock()
	return append([]string(nil), view.names...)
}

// The view's mapped segments are what keeps a deleted segment's disk space
// held, and nothing outside the package can observe them.
func TestReleaseStaleDropsSegmentsAMergeDeleted(t *testing.T) {
	indexDir := t.TempDir()
	publishForView(t, indexDir, "entry-a", "entry-b", "entry-c")
	searchOnce(t, indexDir)
	held := mappedSegments(indexDir)
	if len(held) != 1 {
		t.Fatalf("a search should leave its segment mapped for the next, got %v", held)
	}

	if err := ReleaseStale(indexDir); err != nil {
		t.Fatal(err)
	}
	if got := mappedSegments(indexDir); len(got) != 1 {
		t.Errorf("an unchanged store should stay mapped, got %v", got)
	}

	manifest, err := LoadManifest(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	delete(manifest.Entries, "entry-a")
	delete(manifest.Entries, "entry-b")
	if err := manifest.Save(indexDir); err != nil {
		t.Fatal(err)
	}
	other := &storeView{dir: indexDir}
	if err := func() error {
		lock := lockFile(indexDir)
		if err := lock.Lock(); err != nil {
			return err
		}
		defer func() { _ = lock.Unlock() }()
		return other.refresh()
	}(); err != nil {
		t.Fatal(err)
	}
	if got := mappedSegments(indexDir); len(got) != 1 || got[0] != held[0] {
		t.Fatalf("a merge in another process should not touch this process's view, got %v", got)
	}

	if err := ReleaseStale(indexDir); err != nil {
		t.Fatal(err)
	}
	if got := mappedSegments(indexDir); len(got) != 0 {
		t.Errorf("a changed store should release the mapped segments, got %v", got)
	}
}
