package finders_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/finders"
)

func writeEntryFile(t *testing.T, graphDir, id, content string) string {
	t.Helper()
	path := filepath.Join(graphDir, id[0:4], id[4:6], id[6:8]+"-"+id[9:]+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Entries are read and parsed concurrently; each must still carry exactly its
// own attachments, and one without an attachment directory none.
func TestLoadGraphGivesEveryEntryItsOwnAttachments(t *testing.T) {
	graphDir := t.TempDir()
	want := map[string]int{}
	for i := range 60 {
		id := fmt.Sprintf("20260406-1155%02d-s-tac-a%02d", i, i)
		entryPath := writeEntryFile(t, graphDir, id, "---\ntype: signal\nlayer: tactical\n---\n\nEntry "+id+".")
		want[id] = i % 7
		for n := range i % 7 {
			attachDir := filepath.Join(filepath.Dir(entryPath), id[6:8]+"-"+id[9:])
			if err := os.MkdirAll(attachDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(attachDir, fmt.Sprintf("note-%d.md", n)), []byte("note"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	g, err := finders.New(finders.Options{}).LoadGraph(graphDir)
	if err != nil {
		t.Fatal(err)
	}
	for id, count := range want {
		entry := g.ByID[id]
		if entry == nil {
			t.Fatalf("entry %s should load", id)
		}
		if len(entry.Attachments) != count {
			t.Errorf("entry %s should carry its own %d attachments, got %v", id, count, entry.Attachments)
		}
		for _, attachment := range entry.Attachments {
			if filepath.Base(filepath.Dir(attachment)) != id[6:8]+"-"+id[9:] {
				t.Errorf("entry %s carries another entry's attachment %s", id, attachment)
			}
		}
	}
}

func TestLoadGraphFailsOnAnUnreadableEntry(t *testing.T) {
	graphDir := t.TempDir()
	writeEntryFile(t, graphDir, "20260406-115500-s-tac-aaa", "---\ntype: signal\nlayer: tactical\n---\n\nReadable.")
	locked := writeEntryFile(t, graphDir, "20260406-115501-s-tac-bbb", "---\ntype: signal\nlayer: tactical\n---\n\nLocked.")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })

	if _, err := finders.New(finders.Options{}).LoadGraph(graphDir); err == nil {
		t.Error("an entry file that cannot be read should fail the load")
	}
}
