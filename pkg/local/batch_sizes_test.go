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

// A size another process lowered is where the next one starts; Lower never
// raises one, and a smaller one another process wrote meanwhile wins.
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

// Raise lands only on the size the file still holds, and a change keeps
// what another process wrote since this one read the file.
func TestFilesystemBatchSizes_RaisesTheSizeTheFileStillHolds(t *testing.T) {
	const key = "chat:haiku"
	cases := []struct {
		name string
		// meanwhile is what another process changes after this one read 4.
		meanwhile func(sizes *localadapter.FilesystemBatchSizes) error
		change    func(sizes *localadapter.FilesystemBatchSizes) error
		want      map[string]int
	}{
		{"the size still held doubles",
			func(*localadapter.FilesystemBatchSizes) error { return nil },
			func(sizes *localadapter.FilesystemBatchSizes) error { return sizes.Raise(key, 4, 8) },
			map[string]int{key: 8}},
		{"a size lowered meanwhile stays",
			func(sizes *localadapter.FilesystemBatchSizes) error { return sizes.Lower(key, 2) },
			func(sizes *localadapter.FilesystemBatchSizes) error { return sizes.Raise(key, 4, 8) },
			map[string]int{key: 2}},
		{"a size raised meanwhile outlives a change to another key",
			func(sizes *localadapter.FilesystemBatchSizes) error { return sizes.Raise(key, 4, 8) },
			func(sizes *localadapter.FilesystemBatchSizes) error { return sizes.Lower("chat:gpt", 25) },
			map[string]int{key: 8, "chat:gpt": 25}},
		{"a key the file does not hold stays without a size",
			func(*localadapter.FilesystemBatchSizes) error { return nil },
			func(sizes *localadapter.FilesystemBatchSizes) error { return sizes.Raise("chat:gpt", 0, 8) },
			map[string]int{key: 4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "chat-batch-sizes.json")
			open := func() *localadapter.FilesystemBatchSizes {
				sizes, err := localadapter.NewFilesystemBatchSizes(path)
				if err != nil {
					t.Fatal(err)
				}
				return sizes
			}
			if err := open().Lower(key, 4); err != nil {
				t.Fatal(err)
			}
			sizes, other := open(), open()
			if err := tc.meanwhile(other); err != nil {
				t.Fatal(err)
			}
			if err := tc.change(sizes); err != nil {
				t.Fatal(err)
			}
			stored := open()
			for _, name := range []string{key, "chat:gpt"} {
				got, _ := stored.Size(name)
				if got != tc.want[name] {
					t.Errorf("the file holds %d for %s, want %d", got, name, tc.want[name])
				}
				if held, _ := sizes.Size(name); held != got {
					t.Errorf("the process holds %d for %s, the file %d: a change should leave it at the file's size", held, name, got)
				}
			}
		})
	}
}
