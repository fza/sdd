package local

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The cache's trust rule turns on how old a change is, so these tests read the
// cache at a later clock rather than sleeping past racyWindow.
func writeGraphFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func cachedRevision(t *testing.T, cache *revisionCache, dir string, now time.Time) string {
	t.Helper()
	revision, err := cache.currentAt(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	want, err := graphDirectoryRevision(dir)
	if err != nil {
		t.Fatal(err)
	}
	if revision != want {
		t.Fatalf("the cached revision should equal the content revision, got %s want %s", revision, want)
	}
	return revision
}

func TestRevisionCacheReusesTheRevisionOfAnUnchangedGraph(t *testing.T) {
	dir := t.TempDir()
	writeGraphFile(t, dir, "2026/10/20-100000-s-tac-aaa.md", "alpha")
	writeGraphFile(t, dir, "2026/10/20-100000-s-tac-aaa/notes.md", "notes")
	later := time.Now().Add(time.Hour)
	var cache revisionCache

	first := cachedRevision(t, &cache, dir, later)
	if cache.revision != first {
		t.Fatal("a graph older than the racy window should be remembered")
	}
	if again := cachedRevision(t, &cache, dir, later); again != first {
		t.Errorf("an unchanged graph should keep its revision, got %s", again)
	}
}

func TestRevisionCacheSeesEveryKindOfChange(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, dir, entry string)
	}{
		{"content with size and mtime restored", func(t *testing.T, _, entry string) {
			info, err := os.Stat(entry)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(entry, []byte("ALPHA"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(entry, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
		}},
		{"added file", func(t *testing.T, dir, _ string) {
			writeGraphFile(t, dir, "2026/10/20-100001-s-tac-bbb.md", "beta")
		}},
		{"removed file", func(t *testing.T, _, entry string) {
			if err := os.Remove(entry); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			entry := writeGraphFile(t, dir, "2026/10/20-100000-s-tac-aaa.md", "alpha")
			later := time.Now().Add(time.Hour)
			var cache revisionCache
			before := cachedRevision(t, &cache, dir, later)

			tc.change(t, dir, entry)
			if after := cachedRevision(t, &cache, dir, later); after == before {
				t.Error("a changed graph should get a new revision")
			}
		})
	}
}

func TestRevisionCacheDoesNotRememberAGraphChangedJustNow(t *testing.T) {
	dir := t.TempDir()
	writeGraphFile(t, dir, "2026/10/20-100000-s-tac-aaa.md", "alpha")
	var cache revisionCache

	cachedRevision(t, &cache, dir, time.Now())
	if cache.revision != "" {
		t.Error("a file changed within the racy window should not be trusted to its stamp")
	}
}
