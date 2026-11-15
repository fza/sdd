package handlers

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// SearchIndexOpener opens a read view of the search index for one command. A
// nil index with a nil error means no index is built.
type SearchIndexOpener interface {
	OpenSearchIndex(ctx context.Context) (finders.VectorIndex, func() error, error)
}

// SearchIndexDir opens the search index stored in Dir, waiting at most Bound
// for a writer to release it.
type SearchIndexDir struct {
	Dir   string
	Bound time.Duration
}

func (d SearchIndexDir) OpenSearchIndex(ctx context.Context) (finders.VectorIndex, func() error, error) {
	_, err := os.Stat(d.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading the search index: %w", err)
	}
	snapshot, err := index.OpenSearchSnapshot(ctx, d.Dir, d.Bound)
	if err != nil {
		return nil, nil, fmt.Errorf("opening the search index: %w", err)
	}
	return snapshot, snapshot.Close, nil
}

// openTitleSearch opens the vector search a title call's similar titles come
// from. It returns a nil finder without an embedder or a built index.
func (h *Handler) openTitleSearch(ctx context.Context, graph *model.Graph) (*finders.SearchFinder, func() error, error) {
	if h.embedder == nil || h.searchIndex == nil {
		return nil, func() error { return nil }, nil
	}
	store, closeStore, err := h.searchIndex.OpenSearchIndex(ctx)
	if err != nil || store == nil {
		return nil, func() error { return nil }, err
	}
	finder := finders.NewSearchFinder(finders.SearchFinderOptions{
		Graph:      graph,
		GraphDir:   h.graphDir,
		Embedder:   h.embedder,
		IndexStore: store,
	})
	return finder, closeStore, nil
}

// similarTitles returns the titles of the titled entries nearest entry's body,
// none from a nil finder.
func similarTitles(ctx context.Context, finder *finders.SearchFinder, entry *model.Entry) ([]string, error) {
	if finder == nil {
		return nil, nil
	}
	maxRunes, err := finder.StoredMaxRunes()
	if err != nil {
		return nil, fmt.Errorf("reading the search index's rune cap: %w", err)
	}
	q, ok := query.SimilarTitles(entry, maxRunes)
	if !ok {
		return nil, nil
	}
	result, err := finder.Search(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("searching titles similar to %s: %w", entry.ID, err)
	}
	return result.Titles(), nil
}
