package cliapp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/index"
)

// distinctStores is reached through `sdd index gc`, which needs a live
// embedding provider; the store selection it applies is tested here.
func TestDistinctStoresCountsALinkedStoreOnce(t *testing.T) {
	cache := t.TempDir()
	real := index.StoreDir(cache, "example.test/acme/app", "embedder")
	err := os.MkdirAll(real, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = index.LinkKey(cache, "local/0123456789ab", "example.test/acme/app")
	if err != nil {
		t.Fatal(err)
	}
	other := index.StoreDir(cache, "example.test/acme/other", "embedder")
	err = os.MkdirAll(other, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	stores := distinctStores([]indexStore{
		{label: "local", indexDir: index.StoreDir(cache, "local/0123456789ab", "embedder")},
		{label: "example.test/acme/app", indexDir: real},
		{label: "example.test/acme/other", indexDir: filepath.Clean(other)},
	})
	if len(stores) != 2 || stores[0].label != "local" || stores[1].label != "example.test/acme/other" {
		t.Errorf("a store reached under two keys should be listed once, under the first, got %+v", stores)
	}
}
