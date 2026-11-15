package application

import (
	"context"
	"errors"

	"github.com/networkteam/sdd/pkg/application/types"
)

type IndexNamespace = types.IndexNamespace
type CanonicalChunk = types.CanonicalChunk
type IndexedChunk = types.IndexedChunk
type StoredChunkRef = types.StoredChunkRef
type StoredEntryRef = types.StoredEntryRef
type ScoredChunkHit = types.ScoredChunkHit

type SearchIndexStore interface {
	Manifest(context.Context, IndexNamespace) ([]StoredChunkRef, error)
	Reconcile(context.Context, IndexNamespace, string, []IndexedChunk, []string) error
	Nearest(context.Context, []IndexNamespace, []float32, int) ([]ScoredChunkHit, error)
}

// SearchIndexEntryManifest is an optional capability a persistent store
// implements so the application can reconcile on entry presence (monotonic
// accumulation of immutable-entry chunks) instead of chunk-identity
// comparison. A store that does not implement it falls back to the
// compatibility reconciliation path in vector search.
type SearchIndexEntryManifest interface {
	IndexedEntries(context.Context, IndexNamespace) ([]StoredEntryRef, error)
}

// SearchIndexRunes is an optional capability a persistent store implements to
// report the rune caps its rows were chunked under, newest first: the cap
// fills chunk under, then the cap it replaced while a re-embed is pending
// (index.RuneCaps.ReadMaxRunes). Reads hash under each and read one version per
// entry (model.ReadableVersion), the newest among their hits. A zero cap reads
// uncapped.
type SearchIndexRunes interface {
	ReadMaxRunes(context.Context, IndexNamespace) ([]int, error)
}

// SearchIndexRunesRecorder is the write-side counterpart of SearchIndexRunes.
// A fill records the cap it chunks under before it writes rows, keeping the
// cap it replaces readable, and settles the cap once it has published every
// entry under it. A store without it cannot take a fill.
type SearchIndexRunesRecorder interface {
	RecordMaxRunes(context.Context, IndexNamespace, int) error
	SettleMaxRunes(context.Context, IndexNamespace, int) error
}

// SearchIndexFillLock is an optional store capability admitting one filler per
// namespace across processes. BeginFill returns ErrSearchIndexFillBusy at once
// while another filler holds the namespace, unless wait is set; release ends
// the fill. A store without it lets fills overlap.
type SearchIndexFillLock interface {
	BeginFill(ctx context.Context, namespace IndexNamespace, wait bool) (release func() error, err error)
}

// ErrSearchIndexFillBusy reports that another process fills the namespace.
var ErrSearchIndexFillBusy = errors.New("sdd: another process is filling the search index")

type SearchEntryVersion = types.SearchEntryVersion
type SearchEntryDescriptor = types.SearchEntryDescriptor

// SearchIndexEntryStore publishes complete entry versions, including versions
// with no chunks. Unpublished chunks must be invisible to both EntryPublished
// and Nearest. Publication is atomic, durable on success, and idempotent under
// concurrent calls. A returned error may have committed; callers recheck.
// Implementations validate all identities and vectors before publication.
type SearchIndexEntryStore interface {
	EntryPublished(context.Context, SearchEntryVersion) (bool, error)
	PublishEntry(context.Context, SearchEntryVersion, []IndexedChunk) error
}
