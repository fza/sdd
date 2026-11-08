package index_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/index"
)

const linkFingerprint = "embedder-a"

func realStore(t *testing.T, cacheRoot, key string) string {
	t.Helper()
	dir := index.StoreDir(cacheRoot, key, linkFingerprint)
	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{}"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// linkKey is the repo key the tests link to a store.
const linkKey = "local/0123456789ab"

func linkPath(cacheRoot string) string {
	return filepath.Join(cacheRoot, "index", filepath.FromSlash(linkKey))
}

func isLink(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func markers(t *testing.T, cacheRoot string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(cacheRoot, "index", ".links"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var paths []string
	for _, entry := range entries {
		paths = append(paths, filepath.Join(cacheRoot, "index", ".links", entry.Name()))
	}
	return paths
}

func age(t *testing.T, path string, by time.Duration) {
	t.Helper()
	then := time.Now().Add(-by)
	err := os.Chtimes(path, then, then)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLinkKey(t *testing.T) {
	t.Run("the new key reads the old store through a relative link", func(t *testing.T) {
		cache := t.TempDir()
		old := realStore(t, cache, "example.test/acme/app")
		err := index.LinkKey(cache, "local/0123456789ab", "example.test/acme/app")
		if err != nil {
			t.Fatal(err)
		}
		link := linkPath(cache)
		target, err := os.Readlink(link)
		if err != nil || filepath.IsAbs(target) {
			t.Fatalf("the new key should be a relative symlink, got %q: %v", target, err)
		}
		linked := index.StoreDir(cache, "local/0123456789ab", linkFingerprint)
		if _, err := os.Stat(filepath.Join(linked, "manifest.json")); err != nil {
			t.Errorf("the store should be reachable under the new key: %v", err)
		}
		if index.RealStoreDir(linked) != index.RealStoreDir(old) {
			t.Error("both keys should resolve to one store")
		}
		if len(markers(t, cache)) != 1 {
			t.Error("the link should get a use marker")
		}
	})
	t.Run("a link to a link points at the real store", func(t *testing.T) {
		cache := t.TempDir()
		realStore(t, cache, "example.test/acme/app")
		err := index.LinkKey(cache, "example.test/acme/app-sdd", "example.test/acme/app")
		if err != nil {
			t.Fatal(err)
		}
		err = index.LinkKey(cache, "local/0123456789ab", "example.test/acme/app-sdd")
		if err != nil {
			t.Fatal(err)
		}
		link := linkPath(cache)
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatal(err)
		}
		if isLink(t, filepath.Join(filepath.Dir(link), target)) {
			t.Errorf("links should never chain, got a link to %s", target)
		}
	})
	t.Run("a real directory at the new key is left alone", func(t *testing.T) {
		cache := t.TempDir()
		realStore(t, cache, "example.test/acme/app")
		kept := realStore(t, cache, "local/0123456789ab")
		err := index.LinkKey(cache, "local/0123456789ab", "example.test/acme/app")
		if err != nil {
			t.Fatal(err)
		}
		if isLink(t, linkPath(cache)) {
			t.Error("a real store directory should not become a link")
		}
		if _, err := os.Stat(filepath.Join(kept, "manifest.json")); err != nil {
			t.Errorf("the real store should keep its content: %v", err)
		}
	})
	t.Run("no store links nothing", func(t *testing.T) {
		cache := t.TempDir()
		err := index.LinkKey(cache, "local/0123456789ab", "example.test/acme/app")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(linkPath(cache)); err == nil {
			t.Error("a key without a store should get no link")
		}
	})
}

func TestOpenStoreDirSweepsUnusedLinks(t *testing.T) {
	const idle = 31 * 24 * time.Hour
	setUp := func(t *testing.T) (cache, real string) {
		t.Helper()
		cache = t.TempDir()
		real = realStore(t, cache, "example.test/acme/app")
		err := index.LinkKey(cache, "local/0123456789ab", "example.test/acme/app")
		if err != nil {
			t.Fatal(err)
		}
		return cache, real
	}
	t.Run("a link unused for 30 days goes with its marker", func(t *testing.T) {
		cache, real := setUp(t)
		age(t, markers(t, cache)[0], idle)
		_, err := index.OpenStoreDir(cache, "example.test/acme/app", linkFingerprint)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(linkPath(cache)); err == nil {
			t.Error("the unused link should be swept")
		}
		if len(markers(t, cache)) != 0 {
			t.Error("the marker should go with its link")
		}
		if _, err := os.Stat(filepath.Join(real, "manifest.json")); err != nil {
			t.Errorf("the sweep should never touch the real store: %v", err)
		}
	})
	t.Run("a recently used link stays", func(t *testing.T) {
		cache, _ := setUp(t)
		age(t, markers(t, cache)[0], 29*24*time.Hour)
		_, err := index.OpenStoreDir(cache, "example.test/acme/app", linkFingerprint)
		if err != nil {
			t.Fatal(err)
		}
		if !isLink(t, linkPath(cache)) {
			t.Error("a link used within 30 days should stay")
		}
	})
	t.Run("an open through the link marks it used", func(t *testing.T) {
		cache, _ := setUp(t)
		marker := markers(t, cache)[0]
		age(t, marker, idle)
		dir, err := index.OpenStoreDir(cache, "local/0123456789ab", linkFingerprint)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
			t.Errorf("the open should reach the store through the link: %v", err)
		}
		info, err := os.Stat(marker)
		if err != nil || time.Since(info.ModTime()) > time.Hour {
			t.Errorf("the open should mark the link used: %v", err)
		}
		if !isLink(t, linkPath(cache)) {
			t.Error("a link just used should not be swept")
		}
	})
	t.Run("a real directory behind a stale marker stays", func(t *testing.T) {
		cache, _ := setUp(t)
		marker := markers(t, cache)[0]
		err := os.Remove(linkPath(cache))
		if err != nil {
			t.Fatal(err)
		}
		kept := realStore(t, cache, "local/0123456789ab")
		age(t, marker, idle)
		_, err = index.OpenStoreDir(cache, "example.test/acme/app", linkFingerprint)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(kept, "manifest.json")); err != nil {
			t.Errorf("the sweep should never remove a real directory: %v", err)
		}
		if len(markers(t, cache)) != 0 {
			t.Error("the stale marker should go")
		}
	})
}
