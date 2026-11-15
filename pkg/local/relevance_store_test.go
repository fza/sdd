package local_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/segment"
	"github.com/networkteam/sdd/pkg/local"
	"github.com/networkteam/sdd/pkg/sddtest"
)

func TestFilesystemRelevanceStoreConformance(t *testing.T) {
	sddtest.RunRelevanceStoreTests(t, func(t *testing.T) sddtest.RelevanceStoreFixture {
		store, err := local.NewFilesystemRelevanceStore(filepath.Join(t.TempDir(), "relevance"))
		if err != nil {
			t.Fatal(err)
		}
		return sddtest.RelevanceStoreFixture{Store: store}
	})
}

func TestMemoryRelevanceStoreConformance(t *testing.T) {
	sddtest.RunRelevanceStoreTests(t, func(*testing.T) sddtest.RelevanceStoreFixture {
		return sddtest.RelevanceStoreFixture{Store: local.NewMemoryRelevanceStore()}
	})
}

func TestFilesystemRelevanceStore_WritesOneSegmentFilePerAppend(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "relevance")
	store, err := local.NewFilesystemRelevanceStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.Append(t.Context(), []byte("segment")); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	segments := 0
	for _, entry := range entries {
		if segment.IsName(entry.Name()) {
			segments++
		}
	}
	if segments != 2 || len(entries) != 2 {
		t.Fatalf("%d files, %d segments after two appends: each append should add one segment file and nothing else", len(entries), segments)
	}
}
