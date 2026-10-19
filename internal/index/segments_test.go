package index_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/index"
)

func segmentVector(seed int) []float32 {
	return []float32{float32(seed%5) + 0.5, float32(seed%3) - 1, float32(seed%7) * 0.25, 2}
}

func segmentRows(entryID string, seed int) []index.Row {
	summary := fillRow(entryID, segmentVector(seed))
	body := fillRow(entryID, segmentVector(seed+11))
	body.ChunkID, body.IsSummary, body.Depth = entryID+"#body-0", false, 2
	body.Breadcrumb = []string{"Section", "Detail"}
	body.Text, body.Body = "body of "+entryID, "body of "+entryID
	return []index.Row{summary, body}
}

// publishEntries writes each entry's rows and then the manifest naming them,
// as every writer does.
func publishEntries(t *testing.T, indexDir string, entryIDs ...string) {
	t.Helper()
	if err := writeEntries(t.Context(), indexDir, entryIDs...); err != nil {
		t.Fatalf("publishing entries: %v", err)
	}
}

func writeEntries(ctx context.Context, indexDir string, entryIDs ...string) error {
	return index.WriteStore(ctx, indexDir, func(idx *index.Index) error {
		manifest, err := index.LoadManifest(indexDir)
		if err != nil {
			return err
		}
		for _, entryID := range entryIDs {
			rows := segmentRows(entryID, len(entryID)+int(entryID[len(entryID)-1]))
			if err := idx.UpsertEntry(ctx, entryID, nil, rows); err != nil {
				return err
			}
			manifest.AddVersion(entryID, index.EntryVersion{Hash: rows[0].EntryHash, Fingerprint: rows[0].ModelFingerprint, ChunkIDs: []string{rows[0].ChunkID, rows[1].ChunkID}})
		}
		return manifest.Save(indexDir)
	})
}

func unpublishEntries(t *testing.T, indexDir string, entryIDs ...string) {
	t.Helper()
	manifest, err := index.LoadManifest(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entryID := range entryIDs {
		delete(manifest.Entries, entryID)
	}
	if err := manifest.Save(indexDir); err != nil {
		t.Fatal(err)
	}
}

func segmentFiles(t *testing.T, indexDir string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(indexDir, "segments", "*.seg"))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func snapshotQuery(t *testing.T, indexDir string, query []float32, n int) []index.Hit {
	t.Helper()
	snapshot, err := index.OpenSearchSnapshot(t.Context(), indexDir, time.Second)
	if err != nil {
		t.Fatalf("opening the snapshot: %v", err)
	}
	defer func() { _ = snapshot.Close() }()
	hits, err := snapshot.Query(t.Context(), query, n)
	if err != nil {
		t.Fatalf("querying the snapshot: %v", err)
	}
	return hits
}

func chromemQuery(t *testing.T, indexDir string, query []float32, n int) []index.Hit {
	t.Helper()
	idx, err := index.OpenWithin(t.Context(), indexDir, time.Second)
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}
	hits, err := idx.Query(t.Context(), query, n)
	if err != nil {
		t.Fatalf("querying the store: %v", err)
	}
	return hits
}

func hitIDs(hits []index.Hit) []string {
	ids := make([]string, len(hits))
	for i, hit := range hits {
		ids[i] = hit.ChunkID
	}
	return ids
}

func TestSearchSnapshotMatchesChromemQuery(t *testing.T) {
	indexDir := t.TempDir()
	publishEntries(t, indexDir, "entry-a", "entry-b", "entry-c", "entry-d", "entry-e")
	query := []float32{1, -0.5, 0.75, 2}
	want := chromemQuery(t, indexDir, query, 6)

	tests := []struct {
		name    string
		refresh bool
	}{
		{name: "rows decoded from their files", refresh: false},
		{name: "rows read from segments", refresh: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.refresh {
				if err := index.RefreshSegments(t.Context(), indexDir); err != nil {
					t.Fatalf("refreshing segments: %v", err)
				}
				if len(segmentFiles(t, indexDir)) != 1 {
					t.Fatal("a refresh should write the store's rows into one segment")
				}
			}
			got := snapshotQuery(t, indexDir, query, 6)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("snapshot hits should equal the chromem query's\ngot  %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestRefreshSegmentsWritesOnlyChanges(t *testing.T) {
	indexDir := t.TempDir()
	publishEntries(t, indexDir, "entry-a", "entry-b", "entry-c")
	if err := index.RefreshSegments(t.Context(), indexDir); err != nil {
		t.Fatal(err)
	}
	first := segmentFiles(t, indexDir)

	if err := index.RefreshSegments(t.Context(), indexDir); err != nil {
		t.Fatal(err)
	}
	if got := segmentFiles(t, indexDir); !reflect.DeepEqual(got, first) {
		t.Errorf("a refresh with nothing new should leave the segments alone, got %v", got)
	}

	publishEntries(t, indexDir, "entry-d")
	if err := index.RefreshSegments(t.Context(), indexDir); err != nil {
		t.Fatal(err)
	}
	after := segmentFiles(t, indexDir)
	if len(after) != 2 {
		t.Fatalf("a refresh should add one segment for new rows, got %v", after)
	}
	sizes := map[string]int64{}
	for _, path := range after {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		sizes[path] = info.Size()
	}
	for _, path := range after {
		if path != first[0] && sizes[path] >= sizes[first[0]] {
			t.Error("the added segment should hold only the new rows")
		}
	}
	if got := hitIDs(snapshotQuery(t, indexDir, segmentVector(1), 20)); len(got) != 8 {
		t.Errorf("the snapshot should hold every published row once, got %v", got)
	}
}

func TestRefreshSegmentsCompacts(t *testing.T) {
	t.Run("at the segment limit", func(t *testing.T) {
		indexDir := t.TempDir()
		for i := range 9 {
			publishEntries(t, indexDir, fmt.Sprintf("entry-%d", i))
			if err := index.RefreshSegments(t.Context(), indexDir); err != nil {
				t.Fatal(err)
			}
		}
		if got := segmentFiles(t, indexDir); len(got) != 1 {
			t.Errorf("a refresh past the limit should merge the segments, got %d", len(got))
		}
		if got := snapshotQuery(t, indexDir, segmentVector(1), 50); len(got) != 18 {
			t.Errorf("merging should keep every published row, got %d", len(got))
		}
	})

	t.Run("when unpublished rows outnumber published ones", func(t *testing.T) {
		indexDir := t.TempDir()
		publishEntries(t, indexDir, "entry-a", "entry-b", "entry-c")
		if err := index.RefreshSegments(t.Context(), indexDir); err != nil {
			t.Fatal(err)
		}
		before := segmentFiles(t, indexDir)
		unpublishEntries(t, indexDir, "entry-a", "entry-b")
		if err := index.RefreshSegments(t.Context(), indexDir); err != nil {
			t.Fatal(err)
		}
		after := segmentFiles(t, indexDir)
		if len(after) != 1 || after[0] == before[0] {
			t.Errorf("a refresh should rewrite the segments without the unpublished rows, got %v", after)
		}
		if got := hitIDs(snapshotQuery(t, indexDir, segmentVector(1), 50)); !reflect.DeepEqual(got, hitIDs(chromemQuery(t, indexDir, segmentVector(1), 50))) {
			t.Errorf("the merged segment should hold only the published rows, got %v", got)
		}
	})
}

func TestSearchSnapshotSkipsUnpublishedRows(t *testing.T) {
	indexDir := t.TempDir()
	publishEntries(t, indexDir, "entry-a", "entry-b", "entry-c")
	if err := index.RefreshSegments(t.Context(), indexDir); err != nil {
		t.Fatal(err)
	}
	unpublishEntries(t, indexDir, "entry-b")
	for _, hit := range snapshotQuery(t, indexDir, segmentVector(1), 50) {
		if hit.EntryID == "entry-b" {
			t.Errorf("a row the manifest no longer names should not be served, got %s", hit.ChunkID)
		}
	}
}

func TestSearchSnapshotReadsRowsMissingFromSegments(t *testing.T) {
	indexDir := t.TempDir()
	publishEntries(t, indexDir, "entry-a")
	if err := index.RefreshSegments(t.Context(), indexDir); err != nil {
		t.Fatal(err)
	}
	publishEntries(t, indexDir, "entry-b")
	got := snapshotQuery(t, indexDir, segmentVector(1), 50)
	if !reflect.DeepEqual(hitIDs(got), hitIDs(chromemQuery(t, indexDir, segmentVector(1), 50))) {
		t.Errorf("rows written after the last refresh should be served too, got %v", hitIDs(got))
	}
}

func TestSearchSnapshotRejectsACorruptSegment(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
	}{
		{name: "truncated header", content: []byte("not a segment at all")},
		{name: "unknown format", content: append([]byte("NOTASEGM"), make([]byte, 16)...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indexDir := t.TempDir()
			publishEntries(t, indexDir, "entry-a")
			if err := os.MkdirAll(filepath.Join(indexDir, "segments"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(indexDir, "segments", "broken.seg"), tt.content, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := index.OpenSearchSnapshot(t.Context(), indexDir, time.Second); !errors.Is(err, index.ErrSegmentCorrupt) {
				t.Errorf("a corrupt segment should be reported, got %v", err)
			}
		})
	}
}

func TestOpenSearchSnapshotGivesUpOnAHeldStore(t *testing.T) {
	indexDir := t.TempDir()
	locked, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- index.WriteStore(t.Context(), indexDir, func(*index.Index) error {
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	start := time.Now()
	_, err := index.OpenSearchSnapshot(t.Context(), indexDir, 200*time.Millisecond)
	elapsed := time.Since(start)
	close(release)
	if writeErr := <-done; writeErr != nil {
		t.Fatal(writeErr)
	}
	if !errors.Is(err, index.ErrLockBusy) {
		t.Errorf("a held store should report busy, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("the wait should end at the bound, took %s", elapsed)
	}
}

func TestSearchSnapshotHoldsNoLockOnceOpen(t *testing.T) {
	indexDir := t.TempDir()
	publishEntries(t, indexDir, "entry-a", "entry-b")
	if err := index.RefreshSegments(t.Context(), indexDir); err != nil {
		t.Fatal(err)
	}
	snapshot, err := index.OpenSearchSnapshot(t.Context(), indexDir, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snapshot.Close() }()

	publishEntries(t, indexDir, "entry-c", "entry-d", "entry-e", "entry-f")
	unpublishEntries(t, indexDir, "entry-a", "entry-b", "entry-c", "entry-d", "entry-e")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := index.RefreshSegments(ctx, indexDir); err != nil {
		t.Fatalf("a writer should not wait on an open snapshot: %v", err)
	}
	hits, err := snapshot.Query(t.Context(), segmentVector(1), 50)
	if err != nil {
		t.Fatalf("an open snapshot should still read hits after its segments were merged away: %v", err)
	}
	if len(hits) != 4 {
		t.Errorf("an open snapshot should keep serving the rows it loaded, got %v", hitIDs(hits))
	}
}

func TestConcurrentSearchSnapshotsDuringWrites(t *testing.T) {
	indexDir := t.TempDir()
	publishEntries(t, indexDir, "entry-a", "entry-b")
	if err := index.RefreshSegments(t.Context(), indexDir); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	writerDone := make(chan error, 1)
	go func() {
		for i := range 12 {
			if err := writeEntries(ctx, indexDir, fmt.Sprintf("entry-w%02d", i)); err != nil {
				writerDone <- err
				return
			}
			if err := index.RefreshSegments(ctx, indexDir); err != nil {
				writerDone <- err
				return
			}
		}
		writerDone <- nil
	}()

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			for range 10 {
				snapshot, err := index.OpenSearchSnapshot(ctx, indexDir, 5*time.Second)
				if err != nil {
					errs <- err
					return
				}
				hits, err := snapshot.Query(ctx, segmentVector(1), 100)
				err = errors.Join(err, snapshot.Close())
				if err != nil {
					errs <- err
					return
				}
				seen := map[string]bool{}
				for _, hit := range hits {
					if seen[hit.ChunkID] {
						errs <- fmt.Errorf("row %s served twice", hit.ChunkID)
						return
					}
					seen[hit.ChunkID] = true
				}
				if !seen["entry-a#summary"] || !seen["entry-b#summary"] {
					errs <- fmt.Errorf("rows published before the readers started are missing: %v", hitIDs(hits))
					return
				}
			}
		})
	}
	wg.Wait()
	if err := <-writerDone; err != nil {
		t.Fatalf("writer: %v", err)
	}
	close(errs)
	for err := range errs {
		t.Errorf("concurrent reader: %v", err)
	}
	if got := snapshotQuery(t, indexDir, segmentVector(1), 100); len(got) != 28 {
		t.Errorf("every published row should be served once the writes end, got %d", len(got))
	}
}

func TestSearchSnapshotServesRefreshedRowsFromSegments(t *testing.T) {
	indexDir := t.TempDir()
	publishEntries(t, indexDir, "entry-a", "entry-b")
	want := chromemQuery(t, indexDir, segmentVector(1), 10)
	if err := index.RefreshSegments(t.Context(), indexDir); err != nil {
		t.Fatal(err)
	}
	rowFiles, err := filepath.Glob(filepath.Join(indexDir, "chromem", "*", "*.gob"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range rowFiles {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if got := snapshotQuery(t, indexDir, segmentVector(1), 10); !reflect.DeepEqual(got, want) {
		t.Errorf("a search should read refreshed rows from the segments alone\ngot  %+v\nwant %+v", got, want)
	}
}

func TestSearchSnapshotServesARowOnceAcrossSegments(t *testing.T) {
	indexDir := t.TempDir()
	publishEntries(t, indexDir, "entry-a", "entry-b")
	if err := index.RefreshSegments(t.Context(), indexDir); err != nil {
		t.Fatal(err)
	}
	segment := segmentFiles(t, indexDir)[0]
	data, err := os.ReadFile(segment)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, "segments", "copy.seg"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := hitIDs(snapshotQuery(t, indexDir, segmentVector(1), 50)); len(got) != 4 {
		t.Errorf("a row two segments hold should be served once, got %v", got)
	}
}

func TestRefreshSegmentsWithNothingToDoLeavesReadersAlone(t *testing.T) {
	indexDir := t.TempDir()
	publishEntries(t, indexDir, "entry-a")
	if err := index.RefreshSegments(t.Context(), indexDir); err != nil {
		t.Fatal(err)
	}
	reading, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- index.ReadStore(t.Context(), indexDir, func(*index.Index) error {
			close(reading)
			<-release
			return nil
		})
	}()
	<-reading
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err := index.RefreshSegments(ctx, indexDir)
	close(release)
	if readErr := <-done; readErr != nil {
		t.Fatal(readErr)
	}
	if err != nil {
		t.Errorf("a refresh with nothing to do should not wait on a reader: %v", err)
	}
}

func TestRefreshSegmentsGivesUpOnAHeldStore(t *testing.T) {
	indexDir := t.TempDir()
	publishEntries(t, indexDir, "entry-a")
	locked, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- index.WriteStore(t.Context(), indexDir, func(*index.Index) error {
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	start := time.Now()
	err := index.RefreshSegments(t.Context(), indexDir)
	elapsed := time.Since(start)
	close(release)
	if writeErr := <-done; writeErr != nil {
		t.Fatal(writeErr)
	}
	if !errors.Is(err, index.ErrLockBusy) {
		t.Errorf("a refresh on a held store should report busy, got %v", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("a refresh should not wait out a writer, took %s", elapsed)
	}
}
