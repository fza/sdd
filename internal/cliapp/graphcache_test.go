package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/filestamp"
)

// The graph cache is optional: a cache dir sdd cannot write, as in a sandbox
// or on a full disk, leaves reads complete.
func TestGraphReadsSucceedWithAnUnwritableCacheDir(t *testing.T) {
	f := newAppFixture(t)
	cache := filepath.Join(f.root, "cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cache, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cache, 0o755) })
	// Only files older than the racy window are worth caching.
	time.Sleep(filestamp.RacyWindow + 100*time.Millisecond)

	r := f.run(t, "show", appEntryID)
	if r.err != nil {
		t.Fatalf("sdd show should succeed without a writable graph cache: %v\n%s", r.err, r.stderr)
	}
	if !strings.Contains(r.stdout, appEntryID) {
		t.Errorf("sdd show should print the entry, stdout:\n%s", r.stdout)
	}
	if n := strings.Count(r.stderr, "graph cache"); n != 1 {
		t.Errorf("the unavailable cache should be logged once, stderr:\n%s", r.stderr)
	}
}
