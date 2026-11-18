package local_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/pkg/llm/relevance"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

var _ relevance.BatchSizes = (*localadapter.FilesystemBatchSizes)(nil)

// A size another process lowered is where the next one starts; a size is
// never raised, and a smaller one another process wrote meanwhile wins.
func TestFilesystemBatchSizes_PersistsTheSmallestSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relevance", "chat-batch-sizes.json")
	first, err := localadapter.NewFilesystemBatchSizes(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := first.Size("chat:haiku"); ok {
		t.Fatal("a missing file should hold no size")
	}
	if err := first.Lower("chat:haiku", 50); err != nil {
		t.Fatal(err)
	}
	if err := first.Lower("chat:haiku", 80); err != nil {
		t.Fatal(err)
	}

	second, err := localadapter.NewFilesystemBatchSizes(path)
	if err != nil {
		t.Fatal(err)
	}
	if size, ok := second.Size("chat:haiku"); !ok || size != 50 {
		t.Fatalf("a new process should start at 50, got %d, %v", size, ok)
	}
	if err := second.Lower("chat:gpt", 25); err != nil {
		t.Fatal(err)
	}
	if err := second.Lower("chat:haiku", 12); err != nil {
		t.Fatal(err)
	}
	if err := first.Lower("chat:other", 6); err != nil {
		t.Fatal(err)
	}

	third, err := localadapter.NewFilesystemBatchSizes(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]int{"chat:haiku": 12, "chat:gpt": 25, "chat:other": 6} {
		if size, _ := third.Size(key); size != want {
			t.Errorf("%s holds %d, want %d: a write should keep what the other process wrote", key, size, want)
		}
	}
}

// An unreadable file fails loud instead of starting every judge afresh.
func TestFilesystemBatchSizes_RefusesAnUnreadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat-batch-sizes.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := localadapter.NewFilesystemBatchSizes(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("an unreadable file should be refused naming it, got %v", err)
	}
}
