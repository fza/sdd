package application_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/index"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
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

// PublishEntries publishes the publications before the refused entry, then
// refuses: a fill publishing in batches stops at the same entry.
func (s publishRefusingIndex) PublishEntries(ctx context.Context, publications []types.SearchEntryPublication) error {
	for i, publication := range publications {
		if publication.Version.EntryID != s.refuse {
			continue
		}
		if i > 0 {
			if err := s.PersistentSearchIndexStore.PublishEntries(ctx, publications[:i]); err != nil {
				return err
			}
		}
		return errPublishRefused
	}
	return s.PersistentSearchIndexStore.PublishEntries(ctx, publications)
}

const (
	reembedFirstID  = "20260101-100000-s-tac-aaa"
	reembedSecondID = "20260101-100001-s-tac-bbb"

	// The first entry's body fits one chunk under the wide cap, whose snippet
	// ends inside the first paragraph; the narrow cap splits the body.
	reembedFirstParagraph  = "Alpha paragraph one explains the alpha matters at length, so that the snippet of one wide chunk ends well before the second paragraph begins and never shows any of its words."
	reembedSecondParagraph = "Second alpha paragraph stands alone once the narrow cap splits the body."
	// reembedSecondParagraphEnd shows only in a snippet of the narrow cap's chunks.
	reembedSecondParagraphEnd = "the narrow cap splits the body."
)

func fixedRuneCap(maxRunes int) func(context.Context) (int, error) {
	return func(context.Context) (int, error) { return maxRunes, nil }
}

// fillThenInterruptServeReembed fills the store under one cap, then runs a
// serve-side re-embed under another cap that publishes the first entry only.
func fillThenInterruptServeReembed(t *testing.T) (graphDir, cacheRoot string) {
	t.Helper()
	graphDir, cacheRoot = t.TempDir(), t.TempDir()
	writeCounterEntry(t, graphDir, reembedFirstID, "Alpha topic summary", "## Section\n"+reembedFirstParagraph+"\n\n"+reembedSecondParagraph)
	writeCounterEntry(t, graphDir, reembedSecondID, "Beta topic summary", "## Section\nThe beta entry is about beta matters.")
	counterSearch(t, newRuneCapApp(t, graphDir, cacheRoot, fixedRuneCap(4096)), "alpha")

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
	res, err := reader.Search(t.Context(), sdd.RequestIdentity{Subject: "christopher"}, counterProject, sdd.SearchRequest{SyncMode: sdd.SearchSyncNone, Phrase: "alpha beta", Limit: 10, MaxCitations: 10})
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

func TestServeSearchDuringReembedCitesOnlyTheReembeddedVersion(t *testing.T) {
	graphDir, cacheRoot := fillThenInterruptServeReembed(t)
	results := citedText(readerSearch(t, graphDir, cacheRoot))
	if !strings.Contains(results, reembedSecondParagraphEnd) {
		t.Errorf("the re-embedded entry should be cited from its version under the new cap:\n%s", results)
	}
	if n := strings.Count(results, reembedFirstParagraph[:40]); n != 1 {
		t.Errorf("the re-embedded entry's first paragraph should be cited once, from one version, cited %d times in:\n%s", n, results)
	}
}

func TestServeFillClearsPreviousCapWhenReembedCompletes(t *testing.T) {
	cases := []struct {
		name string
		// completingCap is the cap of the fill that completes the re-embed.
		completingCap int
	}{
		{name: "fill under the new cap", completingCap: 256},
		{name: "fill back under the previous cap", completingCap: 4096},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			graphDir, cacheRoot := fillThenInterruptServeReembed(t)
			counterSearch(t, newRuneCapApp(t, graphDir, cacheRoot, fixedRuneCap(tc.completingCap)), "alpha")
			manifest, err := index.LoadManifest(counterStoreDir(cacheRoot))
			if err != nil {
				t.Fatal(err)
			}
			if manifest.PrevMaxRunes != nil {
				t.Errorf("store should drop the previous cap once the re-embed completes, holds %d", *manifest.PrevMaxRunes)
			}
			if manifest.MaxRunes != tc.completingCap {
				t.Errorf("store should record the completing fill's cap, records %d", manifest.MaxRunes)
			}
		})
	}
}

func TestServeSearchDuringReembedWithoutPublicationSupportFails(t *testing.T) {
	graphDir, cacheRoot := fillThenInterruptServeReembed(t)
	store := localadapter.NewPersistentSearchIndexStore(counterProject, cacheRoot, "counter/repo")
	withoutPublication := struct {
		sdd.SearchIndexStore
		sdd.SearchIndexEntryManifest
		sdd.SearchIndexRunes
		sdd.SearchIndexRunesRecorder
	}{store, store, store, store}
	reader := newRuneCapAppWithIndex(t, graphDir, withoutPublication, fixedRuneCap(256))
	_, err := reader.Search(t.Context(), sdd.RequestIdentity{Subject: "christopher"}, counterProject, sdd.SearchRequest{SyncMode: sdd.SearchSyncNone, Phrase: "alpha beta", Limit: 10, MaxCitations: 3})
	if err == nil {
		t.Error("a search across two caps should fail on a store that cannot report which versions it published")
	}
}

// citedText joins every citation snippet of the result's hits, one per line.
func citedText(res sdd.SearchResult) string {
	var b strings.Builder
	for _, hit := range res.Hits {
		for _, c := range hit.Citations {
			b.WriteString(c.Snippet)
			b.WriteString("\n")
		}
	}
	return b.String()
}
