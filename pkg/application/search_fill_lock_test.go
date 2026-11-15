package application_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/index"
)

// A serve search that finds another process filling the store, a CLI fill
// for one, searches the index as it stands instead of filling it too.
func TestServeSearchSkipsAFillAnotherProcessRuns(t *testing.T) {
	graphDir, cacheRoot := seedCounterGraph(t)
	embeddings := &countingEmbeddings{}
	application := newCounterApp(t, graphDir, cacheRoot, embeddings)
	counterSearch(t, application, "alpha")

	writeCounterEntry(t, graphDir, "20260101-100002-s-tac-ccc", "Alpha follow-up summary", "## Section\nAnother alpha entry.")
	fill, err := index.BeginFill(t.Context(), counterStoreDir(cacheRoot), false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fill.Close() }()
	embeddings.reset()

	res := counterSearch(t, application, "alpha")
	if embeddings.docEmbeds != 0 {
		t.Errorf("a search should leave a running fill alone and embed nothing, embedded %d texts", embeddings.docEmbeds)
	}
	if !slices.Contains(res.EntryIDs, "20260101-100000-s-tac-aaa") {
		t.Errorf("a search beside a running fill should read the index as it stands, found %v", res.EntryIDs)
	}
}
