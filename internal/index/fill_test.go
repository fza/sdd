package index_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/index"
)

func fillRow(entryID string, vector []float32) index.Row {
	text := "text of " + entryID
	return index.Row{EntryID: entryID, EntryHash: "h-" + entryID, ChunkID: entryID + "#summary", Text: text, Body: text, ContentHash: index.HashContent(text), ModelFingerprint: "test/model/4", IsSummary: true, Embedding: vector}
}

// publish writes one entry's row and records its version, rows before the
// manifest as every writer does.
func publish(ctx context.Context, idx *index.Index, manifest *index.Manifest, indexDir, entryID string) error {
	row := fillRow(entryID, []float32{1, 0, 0, 0})
	if err := idx.UpsertEntry(ctx, entryID, nil, []index.Row{row}); err != nil {
		return err
	}
	manifest.AddVersion(entryID, index.EntryVersion{Hash: row.EntryHash, Fingerprint: row.ModelFingerprint, ChunkIDs: []string{row.ChunkID}})
	return manifest.Save(indexDir)
}

func countRows(t *testing.T, indexDir string) int {
	t.Helper()
	idx, err := index.OpenWithin(t.Context(), indexDir, time.Second)
	if err != nil {
		t.Fatalf("reading the store: %v", err)
	}
	return idx.Count()
}

func TestBeginFillAdmitsOneFillerAtATime(t *testing.T) {
	indexDir := t.TempDir()
	first, err := index.BeginFill(t.Context(), indexDir, false)
	if err != nil {
		t.Fatalf("first fill: %v", err)
	}
	if _, err := index.BeginFill(t.Context(), indexDir, false); !errors.Is(err, index.ErrFillBusy) {
		t.Errorf("a second fill should be refused while the first runs, got %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := index.BeginFill(t.Context(), indexDir, false)
	if err != nil {
		t.Errorf("a fill should be admitted once the first closes, got %v", err)
	} else {
		_ = next.Close()
	}
}

func TestOpenWithinGivesUpOnAHeldStore(t *testing.T) {
	indexDir := t.TempDir()
	locked, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- index.WriteStore(t.Context(), indexDir, func(*index.Index) error {
			close(locked)
			select {
			case <-release:
			case <-time.After(5 * time.Second):
			}
			return nil
		})
	}()
	<-locked

	start := time.Now()
	_, err := index.OpenWithin(t.Context(), indexDir, 200*time.Millisecond)
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
	if _, err := index.OpenWithin(t.Context(), indexDir, 200*time.Millisecond); err != nil {
		t.Errorf("a released store should open, got %v", err)
	}
}

func TestFillSessionCommitsInterleaveWithOtherWriters(t *testing.T) {
	ctx := t.Context()
	indexDir := t.TempDir()
	session, err := index.BeginFill(ctx, indexDir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()

	if err := session.Commit(ctx, func(idx *index.Index, manifest *index.Manifest) error {
		return publish(ctx, idx, manifest, indexDir, "entry-a")
	}); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if got := countRows(t, indexDir); got != 1 {
		t.Errorf("a reader between commits should see the committed rows, got %d", got)
	}
	if err := index.WriteStore(ctx, indexDir, func(idx *index.Index) error {
		manifest, err := index.LoadManifest(indexDir)
		if err != nil {
			return err
		}
		return publish(ctx, idx, manifest, indexDir, "entry-other")
	}); err != nil {
		t.Fatalf("a writer between commits: %v", err)
	}
	if err := session.Commit(ctx, func(idx *index.Index, manifest *index.Manifest) error {
		return publish(ctx, idx, manifest, indexDir, "entry-b")
	}); err != nil {
		t.Fatalf("second commit: %v", err)
	}

	manifest, err := index.LoadManifest(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"entry-a", "entry-other", "entry-b"} {
		if len(manifest.Entries[id].Versions) == 0 {
			t.Errorf("a commit should merge into the manifest on disk, %s is missing", id)
		}
	}
	if got := countRows(t, indexDir); got != 3 {
		t.Errorf("every writer's rows should be stored, got %d", got)
	}
}

func TestFillSessionDeletesRowsItWrote(t *testing.T) {
	ctx := t.Context()
	indexDir := t.TempDir()
	session, err := index.BeginFill(ctx, indexDir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	if err := session.Commit(ctx, func(idx *index.Index, manifest *index.Manifest) error {
		return publish(ctx, idx, manifest, indexDir, "entry-a")
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(ctx, func(idx *index.Index, manifest *index.Manifest) error {
		delete(manifest.Entries, "entry-a")
		if err := manifest.Save(indexDir); err != nil {
			return err
		}
		return idx.DeleteEntry(ctx, []string{"entry-a#summary"})
	}); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, indexDir); got != 0 {
		t.Errorf("a row written after the session snapshot should still be deletable, %d left", got)
	}
}

func TestFillSessionWritesAfterAnotherWriterBeforeItsFirstRow(t *testing.T) {
	ctx := t.Context()
	indexDir := t.TempDir()
	session, err := index.BeginFill(ctx, indexDir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	if err := session.Commit(ctx, func(_ *index.Index, manifest *index.Manifest) error {
		manifest.AddVersion("entry-empty", index.EntryVersion{Hash: "h-empty", Fingerprint: "test/model/4"})
		return manifest.Save(indexDir)
	}); err != nil {
		t.Fatalf("a commit without rows: %v", err)
	}
	if err := index.WriteStore(ctx, indexDir, func(idx *index.Index) error {
		manifest, err := index.LoadManifest(indexDir)
		if err != nil {
			return err
		}
		return publish(ctx, idx, manifest, indexDir, "entry-other")
	}); err != nil {
		t.Fatalf("a writer between commits: %v", err)
	}
	if err := session.Commit(ctx, func(idx *index.Index, manifest *index.Manifest) error {
		return publish(ctx, idx, manifest, indexDir, "entry-b")
	}); err != nil {
		t.Errorf("a commit should accept rows next to ones written after its snapshot, got %v", err)
	}
}
