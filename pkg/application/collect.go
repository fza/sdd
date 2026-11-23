package application

import (
	"context"
	"time"

	"github.com/networkteam/slogutils"
)

// CollectSessionsCmd asks for one page of session reclamation. Retention is how
// long a session is kept after it ended; zero removes it at once. Limit zero
// means every match. After continues from a previous result's Next cursor.
type CollectSessionsCmd struct {
	Retention time.Duration
	Limit     int
	After     SessionID
}

// CollectSessionsResult reports removed resources and unreadable sessions. Next
// is the session cursor to continue from, empty when enumeration is complete.
type CollectSessionsResult struct {
	RemovedSessions []SessionID
	RemovedStaged   []SessionRef
	Skipped         []SessionID
	Next            SessionID
}

// CollectSessions removes resources before deleting their session. A failed
// resource deletion leaves the session available for the next pass.
// Collection never executes pending user writes. It removes a session ended
// longer than the retention ago; an open session stays however long it is
// idle, since its handle can still resume it.
func (a *Application) CollectSessions(ctx context.Context, cmd CollectSessionsCmd) (CollectSessionsResult, error) {
	return collectSessions(ctx, a.sessions, a.blobs, a.now(), cmd)
}

// CollectSessionsIn is CollectSessions over stores no Application composes,
// for a sweep across every store on a machine.
func CollectSessionsIn(ctx context.Context, sessions SessionStore, blobs StagedBlobStore, now time.Time, cmd CollectSessionsCmd) (CollectSessionsResult, error) {
	return collectSessions(ctx, legacyEndStore{sessions}, blobs, now, cmd)
}

// OpenSessionsIn lists the sessions of a store no Application composes that
// have not ended, with the ones it cannot read, whose ending it cannot tell.
func OpenSessionsIn(ctx context.Context, sessions SessionStore) ([]SessionID, error) {
	page, err := legacyEndStore{sessions}.List(ctx, SessionFilter{})
	if err != nil {
		return nil, err
	}
	open := page.Unreadable
	for _, session := range page.Sessions {
		if validateStoredSession(session) != nil || session.Metadata.Ended == nil {
			open = append(open, session.Metadata.ID)
		}
	}
	return open, nil
}

func collectSessions(ctx context.Context, sessions SessionStore, blobs StagedBlobStore, now time.Time, cmd CollectSessionsCmd) (CollectSessionsResult, error) {
	cutoff := now.UTC().Add(-cmd.Retention)
	page, err := sessions.List(ctx, SessionFilter{After: cmd.After, Limit: cmd.Limit})
	if err != nil {
		return CollectSessionsResult{}, err
	}
	result := CollectSessionsResult{Skipped: page.Unreadable}
	for _, session := range page.Sessions {
		id := session.Metadata.ID
		due, err := collectable(session, cutoff)
		if err != nil {
			slogutils.FromContext(ctx).Warn("skipping unreadable session", "session", id, "err", err)
			result.Skipped = append(result.Skipped, id)
			continue
		}
		if !due {
			continue
		}
		ref := SessionRef{Subject: session.Metadata.Subject, Session: id}
		if err := blobs.DeleteStaged(ctx, ref); err != nil {
			return result, err
		}
		result.RemovedStaged = append(result.RemovedStaged, ref)
		if err := sessions.Delete(ctx, id); err != nil {
			return result, err
		}
		result.RemovedSessions = append(result.RemovedSessions, id)
	}
	result.Next = page.Next
	return result, nil
}

// collectable reports whether a session is due at cutoff under the rule
// CollectSessions states.
func collectable(session StoredSession, cutoff time.Time) (bool, error) {
	if err := validateStoredSession(session); err != nil {
		return false, err
	}
	end := session.Metadata.Ended
	return end != nil && end.EndedAt.Before(cutoff), nil
}
