package application

import (
	"context"
	"encoding/json"
	"io"
	"slices"
	"sync"
)

// SessionRef addresses one session inside a subject's namespace. Staged blobs
// are scoped to a session, so this is what names their area — there is no owner
// entity, just the two fields that identify whose scaffolding this is.
type SessionRef struct {
	Subject string
	Session SessionID
}

type StagedBlob struct {
	ID       string
	Size     int64
	Filename string
}

// StagedBlobStore owns immutable scratch bytes scoped to a session. Stage's
// metadata describes the upload response; filename references belong in session
// events. DeleteStaged removes every resource before its session is deleted and
// must succeed when that session's resources are already gone. A store that
// also implements StagedBlobDeleter frees each blob once nothing needs it;
// any other store keeps a session's blobs until DeleteStaged.
type StagedBlobStore interface {
	Stage(context.Context, SessionRef, string, io.Reader) (StagedBlob, error)
	Open(context.Context, SessionRef, string) (io.ReadCloser, error)
	DeleteStaged(context.Context, SessionRef) error
}

// StagedBlobDeleter is the optional part of a StagedBlobStore: DeleteBlob
// removes one blob once nothing needs it, and must succeed when it is
// already gone.
type StagedBlobDeleter interface {
	DeleteBlob(context.Context, SessionRef, string) error
}

// RankingStore is the optional part of a StagedBlobStore that keeps what the
// background ranking passes of a session's captures scored: one JSON record
// a pass, under the instance it ranked for, until DeleteStaged. The passes
// end outside the calls that write the session's log, so their records stay
// off it. Rankings returns an instance's records in no order, none for an
// instance without any. With a store that lacks the part the application
// keeps the records in memory, and a server started anew scores the entries
// again.
type RankingStore interface {
	AppendRanking(ctx context.Context, ref SessionRef, instance string, record json.RawMessage) error
	Rankings(ctx context.Context, ref SessionRef, instance string) ([]json.RawMessage, error)
}

// memoryRankings adds the RankingStore part to a staged store without it.
type memoryRankings struct {
	StagedBlobStore
	mu      sync.Mutex
	records map[SessionRef]map[string][]json.RawMessage
}

func (m *memoryRankings) AppendRanking(_ context.Context, ref SessionRef, instance string, record json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records == nil {
		m.records = map[SessionRef]map[string][]json.RawMessage{}
	}
	if m.records[ref] == nil {
		m.records[ref] = map[string][]json.RawMessage{}
	}
	m.records[ref][instance] = append(m.records[ref][instance], slices.Clone(record))
	return nil
}

func (m *memoryRankings) Rankings(_ context.Context, ref SessionRef, instance string) ([]json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.records[ref][instance]), nil
}

func (m *memoryRankings) DeleteStaged(ctx context.Context, ref SessionRef) error {
	m.mu.Lock()
	delete(m.records, ref)
	m.mu.Unlock()
	return m.StagedBlobStore.DeleteStaged(ctx, ref)
}

func (m *memoryRankings) DeleteBlob(ctx context.Context, ref SessionRef, id string) error {
	return deleteBlob(ctx, m.StagedBlobStore, ref, id)
}

// deleteBlob removes one blob when the store can, and keeps it otherwise.
func deleteBlob(ctx context.Context, store StagedBlobStore, ref SessionRef, id string) error {
	deleter, ok := store.(StagedBlobDeleter)
	if !ok {
		return nil
	}
	return deleter.DeleteBlob(ctx, ref, id)
}
