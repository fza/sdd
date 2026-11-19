package application

import (
	"context"
	"io"
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

// deleteBlob removes one blob when the store can, and keeps it otherwise.
func deleteBlob(ctx context.Context, store StagedBlobStore, ref SessionRef, id string) error {
	deleter, ok := store.(StagedBlobDeleter)
	if !ok {
		return nil
	}
	return deleter.DeleteBlob(ctx, ref, id)
}
