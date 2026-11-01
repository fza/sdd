package local_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/index"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
	"github.com/networkteam/sdd/pkg/local"
)

func batchVersion(entryID, hash string) sdd.SearchEntryVersion {
	return sdd.SearchEntryVersion{Namespace: sdd.IndexNamespace{Project: "project", Fingerprint: "space", Metric: "cosine"}, EntryID: entryID, EntryHash: hash}
}

func batchChunk(version sdd.SearchEntryVersion, chunkID string) sdd.IndexedChunk {
	return sdd.IndexedChunk{Chunk: sdd.CanonicalChunk{ID: chunkID, EntryID: version.EntryID, EntryHash: version.EntryHash, Text: chunkID, Body: chunkID}, Vector: []float32{1, 1}}
}

func TestPublishEntriesWritesTheBatchInOneSession(t *testing.T) {
	root := t.TempDir()
	store := local.NewPersistentSearchIndexStore("project", root, "project")
	var batch []types.SearchEntryPublication
	for _, id := range []string{"a", "b", "c"} {
		version := batchVersion(id, "hash")
		batch = append(batch, types.SearchEntryPublication{Version: version, Chunks: []sdd.IndexedChunk{batchChunk(version, id+"-chunk")}})
	}
	if err := store.PublishEntries(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	for _, publication := range batch {
		published, err := store.EntryPublished(t.Context(), publication.Version)
		if err != nil || !published {
			t.Fatalf("every entry of the batch should be published: %s %v", publication.Version.EntryID, err)
		}
	}
	generation, err := os.ReadFile(filepath.Join(index.StoreDir(root, "project", "space"), "generation"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(generation)) != "1" {
		t.Fatalf("the batch should take one write session, store generation reads %q", generation)
	}
}

func TestPublishEntriesRefusesChunkIDConflicts(t *testing.T) {
	first, second := batchVersion("a", "hash"), batchVersion("b", "hash")
	cases := []struct {
		name   string
		stored []types.SearchEntryPublication
		batch  []types.SearchEntryPublication
	}{
		{
			name: "within the batch",
			batch: []types.SearchEntryPublication{
				{Version: first, Chunks: []sdd.IndexedChunk{batchChunk(first, "shared")}},
				{Version: second, Chunks: []sdd.IndexedChunk{batchChunk(second, "shared")}},
			},
		},
		{
			name:   "against a stored version",
			stored: []types.SearchEntryPublication{{Version: first, Chunks: []sdd.IndexedChunk{batchChunk(first, "shared")}}},
			batch:  []types.SearchEntryPublication{{Version: second, Chunks: []sdd.IndexedChunk{batchChunk(second, "shared")}}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := local.NewPersistentSearchIndexStore("project", t.TempDir(), "project")
			if err := store.PublishEntries(t.Context(), tc.stored); err != nil {
				t.Fatal(err)
			}
			if err := store.PublishEntries(t.Context(), tc.batch); err == nil {
				t.Fatal("a chunk ID another version holds should be refused")
			}
			for _, publication := range tc.batch {
				published, err := store.EntryPublished(t.Context(), publication.Version)
				if err != nil || published {
					t.Fatalf("a refused batch should publish none of its entries: %s %v", publication.Version.EntryID, err)
				}
			}
		})
	}
}
