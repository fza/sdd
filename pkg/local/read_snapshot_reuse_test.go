package local_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/syncstate"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/local"
)

func acquireRevision(t *testing.T, graph *local.FilesystemGraphStore) string {
	t.Helper()
	source, err := graph.AcquireSnapshot(t.Context(), sdd.SnapshotReadQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Release(); err != nil {
		t.Fatal(err)
	}
	return source.Snapshot.Revision()
}

// A read of a graph no file changed in since the last read is served from
// that read's snapshot, even while a writer holds the graph lock: a sync
// round holds it across its git fetch and push. The graph is empty, so its
// stamps prove it unchanged without waiting out the racy window.
func TestFilesystemSnapshotOfAnUnchangedGraphWaitsForNoWriter(t *testing.T) {
	dir := t.TempDir()
	graph, err := local.NewFilesystemGraphStore(local.FilesystemGraphStoreOptions{Project: "test", GraphDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	first := acquireRevision(t, graph)
	lock, err := syncstate.LockGraph(dir)
	if err != nil {
		t.Fatal(err)
	}
	type read struct {
		revision string
		err      error
	}
	done := make(chan read, 1)
	go func() {
		source, err := graph.AcquireSnapshot(t.Context(), sdd.SnapshotReadQuery{})
		if err != nil {
			done <- read{err: err}
			return
		}
		done <- read{revision: source.Snapshot.Revision(), err: source.Release()}
	}()
	select {
	case got := <-done:
		syncstate.Unlock(lock)
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.revision != first {
			t.Fatalf("an unchanged graph should keep its revision, got %s want %s", got.revision, first)
		}
	case <-time.After(2 * time.Second):
		syncstate.Unlock(lock)
		<-done
		t.Fatal("a read of an unchanged graph should not wait for the graph lock")
	}
}

func TestFilesystemSnapshotServesAGraphWrittenSinceTheLastRead(t *testing.T) {
	dir := t.TempDir()
	graph, err := local.NewFilesystemGraphStore(local.FilesystemGraphStoreOptions{Project: "test", GraphDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	first := acquireRevision(t, graph)
	path := filepath.Join(dir, "2026", "11", "18-100000-s-tac-abc.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\ntype: signal\nlayer: tactical\nkind: gap\n---\n\nA gap.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if again := acquireRevision(t, graph); again == first {
		t.Fatal("a graph written since the last read should serve its new revision")
	}
}
