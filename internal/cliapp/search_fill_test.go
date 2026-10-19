package cliapp_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/index"
)

// searchLockBound mirrors cliapp.searchLockBound, the longest a search may
// wait on the index lock.
const searchLockBound = 5 * time.Second

const fillBusyNotice = "another process is filling the search index"

const textFallbackNotice = "falling back to text search"

type timedSearch struct {
	result  appResult
	elapsed time.Duration
}

func (f *appFixture) timedSearch(t *testing.T, phrase string) timedSearch {
	t.Helper()
	start := time.Now()
	result := f.run(t, "search", "--query", phrase, "--limit", "50")
	return timedSearch{result: result, elapsed: time.Since(start)}
}

// seedIndexedGraph configures a graph over the fake embedder and indexes two
// entries, so a search finds them while a later fill runs.
func seedIndexedGraph(t *testing.T, f *appFixture, ollama *fakeOllama, endpoint string) {
	t.Helper()
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/fill\ngraph_dir: .sdd/graph\ndefault_branch: main\n"+
		"embedding:\n  provider: ollama\n  model: fake-model\n  batch_size: 2\n  ollama_endpoint: "+endpoint+"\n")
	f.writeEntry(t, "20260101-100000-s-tac-aaa", "Alpha orchard notes", "The alpha entry is about apple orchards.")
	f.writeEntry(t, "20260101-100001-s-tac-bbb", "Beta cultivation notes", "The beta entry mentions alpha once.")
	if result := f.run(t, "index"); result.err != nil {
		t.Fatalf("sdd index: %v\n%s", result.err, result.stderr)
	}
	ollama.reset()
}

func TestSearchSkipsFillAnotherProcessRuns(t *testing.T) {
	ollama := &fakeOllama{}
	server := httptest.NewServer(ollama)
	defer server.Close()
	f := newAppFixture(t)
	seedIndexedGraph(t, f, ollama, server.URL)
	storeDir := f.singleIndexStore(t)
	f.writeEntry(t, "20260102-100000-s-tac-new", "Unindexed topic", "This entry waits for the running fill.")

	session, err := index.BeginFill(t.Context(), storeDir, false)
	if err != nil {
		t.Fatalf("holding the fill lock: %v", err)
	}
	defer func() { _ = session.Close() }()

	search := f.timedSearch(t, "alpha")
	if search.result.err != nil {
		t.Fatalf("search failed: %v\n%s", search.result.err, search.result.stderr)
	}
	if search.elapsed >= searchLockBound {
		t.Errorf("search should not wait for the running fill, took %s", search.elapsed)
	}
	if !strings.Contains(search.result.stderr, fillBusyNotice) {
		t.Errorf("search should report the fill it skipped, stderr: %q", search.result.stderr)
	}
	if strings.Contains(search.result.stderr, textFallbackNotice) {
		t.Errorf("search should read the vector index, stderr: %q", search.result.stderr)
	}
	if !strings.Contains(search.result.stdout, "s-tac-aaa") {
		t.Errorf("search should find the indexed entries, stdout: %q", search.result.stdout)
	}
	if _, inputs := ollama.snapshot(); len(inputs) != 1 {
		t.Errorf("search should embed the query only, embedded %v", inputs)
	}
}

func TestSearchFallsBackToTextWhenIndexStaysLocked(t *testing.T) {
	ollama := &fakeOllama{}
	server := httptest.NewServer(ollama)
	defer server.Close()
	f := newAppFixture(t)
	seedIndexedGraph(t, f, ollama, server.URL)
	storeDir := f.singleIndexStore(t)

	locked, release := make(chan struct{}), make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- index.WriteStore(t.Context(), storeDir, func(*index.Index) error {
			close(locked)
			select {
			case <-release:
			case <-time.After(3 * searchLockBound):
			}
			return nil
		})
	}()
	<-locked

	search := f.timedSearch(t, "alpha orchards")
	close(release)
	if err := <-holderDone; err != nil {
		t.Fatalf("lock holder: %v", err)
	}

	if search.result.err != nil {
		t.Fatalf("search failed: %v\n%s", search.result.err, search.result.stderr)
	}
	if search.elapsed >= searchLockBound+2*time.Second {
		t.Errorf("search should give up on the lock at the bound, took %s", search.elapsed)
	}
	if !strings.Contains(search.result.stderr, textFallbackNotice) {
		t.Errorf("search should report the text fallback, stderr: %q", search.result.stderr)
	}
	if !strings.Contains(search.result.stdout, "s-tac-aaa") {
		t.Errorf("text fallback should find the entry carrying the phrase words, stdout: %q", search.result.stdout)
	}
}
