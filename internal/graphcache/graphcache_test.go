package graphcache_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/filestamp"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/graphcache"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func entryPath(graphDir, id string) string {
	return filepath.Join(graphDir, id[0:4], id[4:6], id[6:8]+"-"+id[9:]+".md")
}

func entryContent(text string) string {
	return "---\ntype: signal\nlayer: tactical\n---\n\n" + text
}

// seedGraph writes a small graph with an attachment and waits until its files
// are older than the window in which a stamp proves nothing.
func seedGraph(t *testing.T) string {
	t.Helper()
	graphDir := t.TempDir()
	writeFile(t, entryPath(graphDir, "20261020-100000-s-tac-aaa"), entryContent("Alpha entry."))
	writeFile(t, entryPath(graphDir, "20261020-100001-s-tac-bbb"), entryContent("Beta entry."))
	writeFile(t, filepath.Join(graphDir, "2026", "10", "20-100001-s-tac-bbb", "notes.md"), "notes")
	time.Sleep(filestamp.RacyWindow + 100*time.Millisecond)
	return graphDir
}

func cacheFile(t *testing.T, cacheRoot string) os.FileInfo {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(cacheRoot, "*.graph"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		return nil
	}
	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// assertLoadsAsFresh loads graphDir through the cache and from its files and
// requires the same entries.
func assertLoadsAsFresh(t *testing.T, graphDir, cacheRoot string) {
	t.Helper()
	cached, err := finders.New(finders.Options{GraphCache: graphcache.New(cacheRoot)}).LoadGraph(graphDir)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := finders.New(finders.Options{}).LoadGraph(graphDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cached.ByID, fresh.ByID) {
		t.Fatal("a load through the cache should yield the entries a load from the files yields")
	}
}

func TestCacheServesAnUnchangedGraph(t *testing.T) {
	t.Parallel()
	graphDir, cacheRoot := seedGraph(t), t.TempDir()
	assertLoadsAsFresh(t, graphDir, cacheRoot)
	saved := cacheFile(t, cacheRoot)
	if saved == nil {
		t.Fatal("a load of a settled graph should save it")
	}

	assertLoadsAsFresh(t, graphDir, cacheRoot)
	if again := cacheFile(t, cacheRoot); again == nil || !os.SameFile(saved, again) {
		t.Error("a load of an unchanged graph should be served from the cache, not rewrite it")
	}
}

func TestCacheMissesOnEveryKindOfChange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, graphDir string)
	}{
		{"entry rewritten with size and mtime restored", func(t *testing.T, graphDir string) {
			path := entryPath(graphDir, "20261020-100000-s-tac-aaa")
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, path, entryContent("ALPHA entry."))
			if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
		}},
		{"entry added", func(t *testing.T, graphDir string) {
			writeFile(t, entryPath(graphDir, "20261020-100002-s-tac-ccc"), entryContent("Gamma entry."))
		}},
		{"entry removed", func(t *testing.T, graphDir string) {
			if err := os.Remove(entryPath(graphDir, "20261020-100000-s-tac-aaa")); err != nil {
				t.Fatal(err)
			}
		}},
		{"attachment added", func(t *testing.T, graphDir string) {
			writeFile(t, filepath.Join(graphDir, "2026", "10", "20-100000-s-tac-aaa", "design.md"), "design")
		}},
		{"attachment removed", func(t *testing.T, graphDir string) {
			if err := os.Remove(filepath.Join(graphDir, "2026", "10", "20-100001-s-tac-bbb", "notes.md")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			graphDir, cacheRoot := seedGraph(t), t.TempDir()
			assertLoadsAsFresh(t, graphDir, cacheRoot)
			saved := cacheFile(t, cacheRoot)

			tc.change(t, graphDir)
			time.Sleep(filestamp.RacyWindow + 100*time.Millisecond)
			assertLoadsAsFresh(t, graphDir, cacheRoot)
			if again := cacheFile(t, cacheRoot); again == nil || os.SameFile(saved, again) {
				t.Error("a changed graph should be read from its files and saved again")
			}
		})
	}
}

func TestCacheRefusesAFileItCannotVerify(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		damage func(data []byte) []byte
	}{
		{"flipped payload byte", func(data []byte) []byte { data[len(data)-1] ^= 0xff; return data }},
		{"altered entry text", func(data []byte) []byte {
			return bytes.Replace(data, []byte("Alpha entry."), []byte("Alpha entrY."), 1)
		}},
		{"other format", func(data []byte) []byte { data[8] = 99; return data }},
		{"truncated", func(data []byte) []byte { return data[:20] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			graphDir, cacheRoot := seedGraph(t), t.TempDir()
			assertLoadsAsFresh(t, graphDir, cacheRoot)
			files, err := filepath.Glob(filepath.Join(cacheRoot, "*.graph"))
			if err != nil || len(files) != 1 {
				t.Fatalf("expected one cache file, got %v (%v)", files, err)
			}
			data, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(files[0], tc.damage(data), 0o644); err != nil {
				t.Fatal(err)
			}
			damaged := cacheFile(t, cacheRoot)

			assertLoadsAsFresh(t, graphDir, cacheRoot)
			if again := cacheFile(t, cacheRoot); again == nil || os.SameFile(damaged, again) {
				t.Error("a cache file that fails verification should be replaced")
			}
		})
	}
}

func TestCacheSkipsAGraphItsStampsCannotProve(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		build func(t *testing.T) string
	}{
		{"just written", func(t *testing.T) string {
			graphDir := t.TempDir()
			writeFile(t, entryPath(graphDir, "20261020-100000-s-tac-aaa"), entryContent("Alpha entry."))
			return graphDir
		}},
		{"symlinked entry", func(t *testing.T) string {
			graphDir, target := t.TempDir(), filepath.Join(t.TempDir(), "entry.md")
			writeFile(t, target, entryContent("Alpha entry."))
			link := entryPath(graphDir, "20261020-100000-s-tac-aaa")
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			time.Sleep(filestamp.RacyWindow + 100*time.Millisecond)
			return graphDir
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			graphDir, cacheRoot := tc.build(t), t.TempDir()
			assertLoadsAsFresh(t, graphDir, cacheRoot)
			if cacheFile(t, cacheRoot) != nil {
				t.Error("a graph its stamps cannot prove should not be cached")
			}
		})
	}
}
