package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/query"
)

type ReconcileSearchIndexCmd = command.ReconcileSearchIndexCmd
type SearchSyncMode = query.SearchSyncMode

const (
	SearchSyncNone  = query.SearchSyncNone
	SearchSyncLocal = query.SearchSyncLocal
	SearchSyncAll   = query.SearchSyncAll
)

// ReconcileSearchIndex maintains the runtime's current graph index. The host
// authorizes the call; this operation does not resolve a request identity.
func (r *ProjectRuntime) ReconcileSearchIndex(ctx context.Context, cmd ReconcileSearchIndexCmd) (err error) {
	namespace, err := r.indexNamespace()
	if err != nil {
		return err
	}
	selected, err := acquireSnapshotForReadBranch(ctx, r, "")
	if err != nil {
		return err
	}
	defer selected.releaseInto(&err)
	snapshot := selected.snapshot
	release, _, err := r.beginFill(ctx, namespace, true)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	maxRunes, err := r.fillRunes(ctx, namespace)
	if err != nil {
		return err
	}
	hashes, err := r.currentEntryHashes(ctx, snapshot.graph.Entries, selected.store, maxRunes)
	if err != nil {
		return err
	}
	return r.reconcileSearchSnapshot(ctx, snapshot, selected.store, namespace, hashes, maxRunes, cmd)
}

// beginFill takes the namespace's fill lock when the store has one. With wait
// false a fill another process runs reports busy, and the caller searches the
// index as it stands.
func (r *ProjectRuntime) beginFill(ctx context.Context, namespace IndexNamespace, wait bool) (release func() error, busy bool, err error) {
	lock, ok := r.options.SearchIndex.(SearchIndexFillLock)
	if !ok {
		return func() error { return nil }, false, nil
	}
	release, err = lock.BeginFill(ctx, namespace, wait)
	if errors.Is(err, ErrSearchIndexFillBusy) {
		slogutils.FromContext(ctx).Info("search index fill running in another process, skipping", "project", namespace.Project)
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return release, false, nil
}

func (r *ProjectRuntime) indexNamespace() (IndexNamespace, error) {
	if r.options.Embedder == nil || r.options.SearchIndex == nil {
		return IndexNamespace{}, errVectorUnavailable
	}
	fingerprint := r.options.Embedder.Fingerprint()
	if fingerprint == "" {
		return IndexNamespace{}, fmt.Errorf("sdd: embedder returned an empty fingerprint")
	}
	return IndexNamespace{Project: r.options.Project.ID, Fingerprint: fingerprint, Metric: "cosine"}, nil
}

func (r *ProjectRuntime) reconcileSearchSnapshot(ctx context.Context, snapshot *Snapshot, store GraphStore, namespace IndexNamespace, hashes map[string]string, maxRunes int, cmd ReconcileSearchIndexCmd) error {
	h := handlers.SearchIndexHandler{
		Store: r.options.SearchIndex, Embedder: r.options.Embedder,
		Graph: snapshot.graph, Revision: snapshot.revision, Namespace: namespace, Hashes: hashes,
		Attachments:     graphStoreAttachmentReader{store: store},
		ExcludeEmbedded: r.options.ExcludeEmbeddedFromIndex,
		MaxRunes:        maxRunes,
	}
	if err := h.Reconcile(ctx, cmd); err != nil {
		return err
	}
	return r.settleRunes(ctx, namespace, maxRunes)
}
