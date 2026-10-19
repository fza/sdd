package local_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/pkg/local"

	app "github.com/networkteam/sdd/pkg/application"
)

// A server write folds its rows into the search segments, so its searches read
// segments alone: with every row file gone, the rows are still found.
func TestPersistentStoreSearchesSegmentsAfterAWrite(t *testing.T) {
	cacheRoot := t.TempDir()
	const project = app.ProjectID("segments")
	ns := app.IndexNamespace{Project: project, Fingerprint: "fp", Metric: "cosine"}
	store := local.NewPersistentSearchIndexStore(project, cacheRoot, "segments/repo")
	chunk := app.IndexedChunk{Chunk: app.CanonicalChunk{
		ID: "e1#v-a#summary", EntryID: "e1", EntryHash: "a", ContentHash: "c1",
		Text: "alpha", Body: "alpha", IsSummary: true,
	}, Vector: []float32{1, 0}}
	if err := store.Reconcile(t.Context(), ns, "r1", []app.IndexedChunk{chunk}, nil); err != nil {
		t.Fatal(err)
	}

	dir := index.StoreDir(cacheRoot, "segments/repo", "fp")
	segments, err := filepath.Glob(filepath.Join(dir, "segments", "*.seg2"))
	if err != nil || len(segments) != 1 {
		t.Fatalf("a write should leave its rows in one segment, got %v (%v)", segments, err)
	}
	rowFiles, err := filepath.Glob(filepath.Join(dir, "chromem", "*", "*.gob"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range rowFiles {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}

	hits, err := store.Nearest(t.Context(), []app.IndexNamespace{ns}, []float32{1, 0}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ChunkID != chunk.Chunk.ID || hits[0].EntryHash != "a" {
		t.Errorf("the search should serve the written row from the segment, got %+v", hits)
	}
}
