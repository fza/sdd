package application_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// writeBeforeAppends lets another attach append before each of a holder's
// next appends, as many times as races holds: a CLI call landing while an
// abandon is in flight, once or on every attempt.
type writeBeforeAppends struct {
	sdd.SessionStore
	races   atomic.Int32
	writing atomic.Bool
	write   func(context.Context)
}

func (s *writeBeforeAppends) Append(ctx context.Context, id sdd.SessionID, version uint64, data sdd.SessionAppend) (uint64, error) {
	if !s.writing.Load() && s.races.Add(-1) >= 0 {
		s.writing.Store(true)
		s.write(ctx)
		s.writing.Store(false)
	}
	return s.SessionStore.Append(ctx, id, version, data)
}

func abandonRaceApp(t *testing.T) (*sdd.Application, *writeBeforeAppends, *countingSessionStore, sdd.RequestIdentity, *sdd.WorkflowSession) {
	t.Helper()
	racing := &writeBeforeAppends{}
	application, sessions, _, _ := newStampWorkflowApp(t, "", "", time.Now, func(store sdd.SessionStore) sdd.SessionStore {
		racing.SessionStore = store
		return racing
	})
	identity := sdd.RequestIdentity{Subject: "christopher"}
	holder, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = holder.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "waiting-test"})
	if err != nil {
		t.Fatal(err)
	}
	racing.write = func(context.Context) {
		cli := attachAsCLI(t, application, identity, holder.ID())
		err := cli.LogRead(t.Context(), identity, "show", []string{"20260713-060000-d-prc-trm"}, nil)
		if err != nil {
			t.Error(err)
		}
	}
	return application, racing, sessions, identity, holder
}

// An abandon racing another attach's append reloads the log and ends the
// session on the second attempt, keeping what the other attach wrote
// (20260914-113737-d-cpt-qst bounds the retry to one).
func TestAbandonFoldsAnAppendRacingIt(t *testing.T) {
	application, racing, sessions, identity, holder := abandonRaceApp(t)
	racing.races.Store(1)
	result, err := application.AbandonWorkflowSession(t.Context(), identity, sdd.WorkflowResumeRequest{SessionID: holder.ID()}, "done here")
	if err != nil {
		t.Fatalf("the abandon should end the session past an append racing it, got %v", err)
	}
	if len(result.Discarded) != 1 {
		t.Fatalf("the running instance should be discarded, got %+v", result.Discarded)
	}
	stored, err := sessions.Load(t.Context(), holder.ID())
	if err != nil {
		t.Fatal(err)
	}
	if stored.Metadata.Ended == nil || stored.Metadata.Ended.Act != sdd.SessionAbandoned {
		t.Fatalf("the session should be recorded as abandoned, got %+v", stored.Metadata.Ended)
	}
	read := false
	for _, event := range stored.Events {
		if strings.Contains(string(event.Payload), "20260713-060000-d-prc-trm") {
			read = true
		}
	}
	if !read {
		t.Fatal("the racing attach's read should stay in the log")
	}
	assertIncreasing(t, workflowSeqs(t, stored))
}

// A second collision surfaces as the conflict it is, with the way on.
func TestAbandonSurfacesASecondCollision(t *testing.T) {
	application, racing, sessions, identity, holder := abandonRaceApp(t)
	racing.races.Store(2)
	_, err := application.AbandonWorkflowSession(t.Context(), identity, sdd.WorkflowResumeRequest{SessionID: holder.ID()}, "done here")
	var appErr *sdd.ApplicationError
	if !errors.As(err, &appErr) || appErr.Code != sdd.ErrorSessionConflict {
		t.Fatalf("a second collision should surface as a session conflict, got %v", err)
	}
	if !strings.Contains(err.Error(), "resume_session") {
		t.Fatalf("the conflict should name the way on, got %v", err)
	}
	stored, err := sessions.Load(t.Context(), holder.ID())
	if err != nil {
		t.Fatal(err)
	}
	if stored.Metadata.Ended != nil {
		t.Fatalf("a failed abandon should leave the session open, got %+v", stored.Metadata.Ended)
	}
}
