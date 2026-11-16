package local_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/index"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/local"
)

// The serve store reads under a moved graph's repo key through the link to
// its store, and each read marks the link used so the sweep keeps it.
func TestPersistentStoreMarksALinkedStoreUsed(t *testing.T) {
	cache := t.TempDir()
	const oldKey, newKey = "example.test/acme/app", "local/0123456789ab"
	namespace := sdd.IndexNamespace{Project: "project", Fingerprint: "space", Metric: "cosine"}
	err := os.MkdirAll(index.StoreDir(cache, oldKey, namespace.Fingerprint), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = index.LinkKey(cache, newKey, oldKey)
	if err != nil {
		t.Fatal(err)
	}
	markers, err := filepath.Glob(filepath.Join(cache, "index", ".links", "*"))
	if err != nil || len(markers) != 1 {
		t.Fatalf("the link should carry one use marker: %v %v", markers, err)
	}
	stale := time.Now().Add(-31 * 24 * time.Hour)
	err = os.Chtimes(markers[0], stale, stale)
	if err != nil {
		t.Fatal(err)
	}

	store := local.NewPersistentSearchIndexStore("project", cache, newKey)
	_, err = store.IndexedEntries(t.Context(), namespace)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(markers[0])
	if err != nil || time.Since(info.ModTime()) > time.Hour {
		t.Errorf("a read through the link should mark it used: %v", err)
	}
}
