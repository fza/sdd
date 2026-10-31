package application

import (
	"context"

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
// report the rune cap its rows were chunked under. Hashes and new rows use that
// cap, so they agree with the rows already stored. Zero means no cap is recorded
// for the namespace; reads then hash uncapped.
type SearchIndexRunes interface {
	MaxRunes(context.Context, IndexNamespace) (int, error)
}

// SearchIndexRunesRecorder is the write-side counterpart of SearchIndexRunes.
// A fill records the cap it chunks under before it writes rows, so reads of
// the store hash under the cap its rows carry. A store without it cannot take
// a fill.
type SearchIndexRunesRecorder interface {
	RecordMaxRunes(context.Context, IndexNamespace, int) error
}

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
