package handlers_test

import (
	"context"
	"testing"

	"github.com/networkteam/sdd/internal/chunking"
	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/application/types"
)

type recordingIndexStore struct{ recorded map[string]string }

func (s *recordingIndexStore) Manifest(context.Context, types.IndexNamespace) ([]types.StoredChunkRef, error) {
	return nil, nil
}

func (s *recordingIndexStore) Reconcile(context.Context, types.IndexNamespace, string, []types.IndexedChunk, []string) error {
	return nil
}

func (s *recordingIndexStore) RecordCheckout(_ context.Context, _ types.IndexNamespace, hashes map[string]string) error {
	s.recorded = hashes
	return nil
}

func TestReconcileRecordsHashesUnderTheStoresRuneCap(t *testing.T) {
	graphDir := t.TempDir()
	const id = "20260101-100000-s-tac-aaa"
	writeGraphEntry(t, graphDir, id, "Alpha orchard notes", "The alpha entry is about apple orchards and the trees that grow in them.")
	graph, err := finders.New(finders.Options{PreflightRunner: noLLM{}, Config: &model.PerRepoConfig{}}).CurrentGraph(graphDir)
	if err != nil {
		t.Fatal(err)
	}
	const maxRunes = 512
	store := &recordingIndexStore{}
	h := handlers.SearchIndexHandler{
		Store: store, Embedder: &slowEmbedder{}, Graph: graph,
		Namespace: types.IndexNamespace{Project: "p", Fingerprint: "f", Metric: "cosine"},
		MaxRunes:  maxRunes,
	}
	if err := h.Reconcile(t.Context(), command.ReconcileSearchIndexCmd{}); err != nil {
		t.Fatal(err)
	}
	want, err := chunking.EntryStateHashCapped(t.Context(), graph.Entries[0], nil, maxRunes)
	if err != nil {
		t.Fatal(err)
	}
	if store.recorded[id] != want {
		t.Error("the checkout record should hold the hash under the rune cap the rows are chunked under")
	}
}
