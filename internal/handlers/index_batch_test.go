package handlers_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/application/types"
	"github.com/networkteam/sdd/pkg/llm/embed"
	"github.com/networkteam/sdd/pkg/local"
)

type batchStore struct {
	*local.MemorySearchIndexStore
	sizes *[]int
}

func (s batchStore) PublishEntries(ctx context.Context, publications []types.SearchEntryPublication) error {
	*s.sizes = append(*s.sizes, len(publications))
	for _, publication := range publications {
		if err := s.PublishEntry(ctx, publication.Version, publication.Chunks); err != nil {
			return err
		}
	}
	return nil
}

func TestReconcilePublishesFinishedEntriesInBatches(t *testing.T) {
	var entries []*model.Entry
	for n := range 130 {
		entries = append(entries, &model.Entry{ID: fmt.Sprintf("e%03d", n), Summary: fmt.Sprintf("Summary %d", n)})
	}
	failed := errors.New("provider unavailable")
	cases := []struct {
		name      string
		failAt    int
		wantSizes []int
		wantErr   error
	}{
		{name: "complete run", wantSizes: []int{64, 64, 2}},
		{name: "embedder fails midway", failAt: 8, wantSizes: []int{64, 6}, wantErr: failed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sizes []int
			store := batchStore{MemorySearchIndexStore: local.NewMemorySearchIndexStore(), sizes: &sizes}
			calls := 0
			inner := embed.EmbedderFunc{Space: "test", Run: func(_ context.Context, req embed.Request) (embed.Result, error) {
				calls++
				if calls == tc.failAt {
					return embed.Result{}, failed
				}
				result := embed.Result{}
				for range req.Texts {
					result.Vectors = append(result.Vectors, []float32{1, 1})
				}
				return result, nil
			}}
			published := 0
			h := handlers.SearchIndexHandler{Graph: model.NewGraph(entries), Namespace: types.IndexNamespace{Project: "test", Fingerprint: "test", Metric: "cosine"}, Store: store, Embedder: handlers.IndexEmbedder{Embedder: inner, BatchSize: 10}}
			err := h.Reconcile(t.Context(), command.ReconcileSearchIndexCmd{OnEntryIndexed: func(string, int) { published++ }})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("reconcile error: %v", err)
			}
			if !reflect.DeepEqual(sizes, tc.wantSizes) {
				t.Fatalf("finished entries should reach the store in full batches, then the rest: %v", sizes)
			}
			total := 0
			for _, size := range tc.wantSizes {
				total += size
			}
			if published != total {
				t.Fatalf("progress should report every published entry, reported %d", published)
			}
		})
	}
}
