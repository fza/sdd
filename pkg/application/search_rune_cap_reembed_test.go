package application_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/index"
	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

var errPublishRefused = errors.New("publication refused")

// publishRefusingIndex refuses to publish one entry, so a fill stops after
// publishing the entries before it.
type publishRefusingIndex struct {
	*localadapter.PersistentSearchIndexStore
	refuse string
}

func (s publishRefusingIndex) PublishEntry(ctx context.Context, version sdd.SearchEntryVersion, chunks []sdd.IndexedChunk) error {
	if version.EntryID == s.refuse {
		return errPublishRefused
	}
	return s.PersistentSearchIndexStore.PublishEntry(ctx, version, chunks)
}

const (
	reembedFirstID  = "20260101-100000-s-tac-aaa"
	reembedSecondID = "20260101-100001-s-tac-bbb"
)

func fixedRuneCap(maxRunes int) func(context.Context) (int, error) {
	return func(context.Context) (int, error) { return maxRunes, nil }
}

// fillThenInterruptServeReembed fills the store under one cap, then runs a
// serve-side re-embed under another cap that publishes the first entry only.
func fillThenInterruptServeReembed(t *testing.T) (graphDir, cacheRoot string) {
	t.Helper()
	graphDir, cacheRoot = seedCounterGraph(t)
	counterSearch(t, newRuneCapApp(t, graphDir, cacheRoot, fixedRuneCap(512)), "alpha")

	refusing := publishRefusingIndex{PersistentSearchIndexStore: localadapter.NewPersistentSearchIndexStore(counterProject, cacheRoot, "counter/repo"), refuse: reembedSecondID}
	reembed := newRuneCapAppWithIndex(t, graphDir, refusing, fixedRuneCap(256))
	_, err := reembed.Search(t.Context(), sdd.RequestIdentity{Subject: "christopher"}, counterProject, sdd.SearchRequest{SyncMode: sdd.SearchSyncAll, Phrase: "alpha", Limit: 5, MaxCitations: 3})
	if !errors.Is(err, errPublishRefused) {
		t.Fatalf("interrupted re-embed should end with the refused publication, got %v", err)
	}
	return graphDir, cacheRoot
}

// readerSearch searches the store as another reader, without filling it.
func readerSearch(t *testing.T, graphDir, cacheRoot string) sdd.SearchResult {
	t.Helper()
	reader := newRuneCapApp(t, graphDir, cacheRoot, fixedRuneCap(256))
	res, err := reader.Search(t.Context(), sdd.RequestIdentity{Subject: "christopher"}, counterProject, sdd.SearchRequest{SyncMode: sdd.SearchSyncNone, Phrase: "alpha beta", Limit: 10, MaxCitations: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	return res
}

func TestServeSearchDuringReembedUnderChangedCapFindsEveryEntry(t *testing.T) {
	graphDir, cacheRoot := fillThenInterruptServeReembed(t)
	got := readerSearch(t, graphDir, cacheRoot).EntryIDs
	for _, id := range []string{reembedFirstID, reembedSecondID} {
		if !slices.Contains(got, id) {
			t.Errorf("search during the re-embed found %v, missing %s", got, id)
		}
	}
}

func TestServeSearchDuringReembedCitesOneVersionPerEntry(t *testing.T) {
	graphDir, cacheRoot := fillThenInterruptServeReembed(t)
	results := readerSearch(t, graphDir, cacheRoot).Results
	if n := strings.Count(results, "The alpha entry is about alpha matters."); n != 1 {
		t.Errorf("the re-embedded entry's body should be cited once, from one version, cited %d times in:\n%s", n, results)
	}
}

func TestServeFillClearsPreviousCapWhenReembedCompletes(t *testing.T) {
	graphDir, cacheRoot := fillThenInterruptServeReembed(t)
	counterSearch(t, newRuneCapApp(t, graphDir, cacheRoot, fixedRuneCap(256)), "alpha")
	manifest, err := index.LoadManifest(counterStoreDir(cacheRoot))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.PrevMaxRunes != nil {
		t.Errorf("store should drop the previous cap once the re-embed completes, holds %d", *manifest.PrevMaxRunes)
	}
}
