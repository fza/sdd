package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// attachAnother resumes the session as a second client attached to it.
func attachAnother(t *testing.T, application *sdd.Application, identity sdd.RequestIdentity, id sdd.SessionID) *sdd.WorkflowSession {
	t.Helper()
	second, _, err := application.ResumeWorkflow(t.Context(), identity, sdd.WorkflowResumeRequest{SessionID: id, ClientName: "second-client"})
	if err != nil {
		t.Fatal(err)
	}
	return second
}

// workflowSeqs returns the engine sequence of every workflow event in the
// stored log, in log order.
func workflowSeqs(t *testing.T, stored sdd.StoredSession) []int {
	t.Helper()
	var seqs []int
	for _, event := range stored.Events {
		if event.Code != sdd.WorkflowEventCode {
			continue
		}
		var decoded struct {
			Seq int `json:"seq"`
		}
		err := json.Unmarshal(event.Payload, &decoded)
		if err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, decoded.Seq)
	}
	return seqs
}

func assertIncreasing(t *testing.T, seqs []int) {
	t.Helper()
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Fatalf("workflow event sequences should rise strictly, got %v", seqs)
		}
	}
}

// writeBetweenLoadAndAppend lets another attach append between a holder's
// load of the log and its append, once ahead of each of the holder's next
// owed appends: the race of another client's call landing while a serve is
// in flight.
type writeBetweenLoadAndAppend struct {
	sdd.SessionStore
	owed    atomic.Int32
	writing atomic.Bool
	write   func(context.Context)
}

func (s *writeBetweenLoadAndAppend) Append(ctx context.Context, id sdd.SessionID, version uint64, data sdd.SessionAppend) (uint64, error) {
	if s.owed.Load() > 0 && s.writing.CompareAndSwap(false, true) {
		s.owed.Add(-1)
		s.write(ctx)
		s.writing.Store(false)
	}
	return s.SessionStore.Append(ctx, id, version, data)
}

// sessionCatchUps is how often one append of a holder catches up with other
// writers before it surfaces the conflict.
const sessionCatchUps = 8

// racingReads opens a holder on the waiting procedure over a store that lets
// another attach log a read ahead of the holder's appends, owed times.
func racingReads(t *testing.T, owed int) (*countingSessionStore, *sdd.WorkflowSession, string, *writeBetweenLoadAndAppend) {
	t.Helper()
	racing := &writeBetweenLoadAndAppend{}
	application, sessions, _, _ := newStampWorkflowApp(t, "", "", time.Now, func(store sdd.SessionStore) sdd.SessionStore {
		racing.SessionStore = store
		return racing
	})
	identity := sdd.RequestIdentity{Subject: "christopher"}
	holder, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	serve, err := holder.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "waiting-test"})
	if err != nil {
		t.Fatal(err)
	}
	racing.write = func(context.Context) {
		second := attachAnother(t, application, identity, holder.ID())
		err := second.LogRead(t.Context(), identity, "show", []string{"20260713-060000-d-prc-trm"}, nil)
		if err != nil {
			t.Error(err)
		}
	}
	racing.owed.Store(int32(owed))
	return sessions, holder, serve.Instance, racing
}

// A holder's append that meets another writer's append on every try catches
// up each time and lands, as long as none of them touches its instance.
func TestWorkflowHolderCatchesUpWithEachAppendRacingItsOwn(t *testing.T) {
	sessions, holder, instance, racing := racingReads(t, sessionCatchUps)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	_, err := holder.Advance(t.Context(), identity, sdd.WorkflowAdvanceRequest{Instance: instance, Report: map[string]any{"body": "one"}})
	if err != nil {
		t.Fatalf("the holder should catch up with every append racing its own, got %v", err)
	}
	if left := racing.owed.Load(); left != 0 {
		t.Fatalf("every racing append should have landed, %d did not", left)
	}
	stored, err := sessions.Load(t.Context(), holder.ID())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored.Events[len(stored.Events)-1].Payload), `"body":"one"`) {
		t.Fatalf("the holder's report should land last, after the racing appends: %s", stored.Events[len(stored.Events)-1].Payload)
	}
	assertIncreasing(t, workflowSeqs(t, stored))
}

// A writer that lands ahead of every try does not hold the holder's call
// forever: past its catch-ups the append surfaces the conflict.
func TestWorkflowHolderSurfacesAConflictPastItsCatchUps(t *testing.T) {
	_, holder, instance, _ := racingReads(t, sessionCatchUps+1)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	_, err := holder.Advance(t.Context(), identity, sdd.WorkflowAdvanceRequest{Instance: instance, Report: map[string]any{"body": "one"}})
	var conflict *sdd.ApplicationError
	if !errors.As(err, &conflict) || conflict.Code != sdd.ErrorSessionConflict {
		t.Fatalf("the append should surface the session conflict once its catch-ups are spent, got %v", err)
	}
}

func TestWorkflowHolderFoldsAnAppendRacingItsOwn(t *testing.T) {
	racing := &writeBetweenLoadAndAppend{}
	application, sessions, _, _ := newStampWorkflowApp(t, "", "", time.Now, func(store sdd.SessionStore) sdd.SessionStore {
		racing.SessionStore = store
		return racing
	})
	identity := sdd.RequestIdentity{Subject: "christopher"}
	holder, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	serve, err := holder.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "waiting-test"})
	if err != nil {
		t.Fatal(err)
	}
	racing.write = func(context.Context) {
		second := attachAnother(t, application, identity, holder.ID())
		err := second.LogRead(t.Context(), identity, "show", []string{"20260713-060000-d-prc-trm"}, nil)
		if err != nil {
			t.Error(err)
		}
	}
	racing.owed.Store(1)
	_, err = holder.Advance(t.Context(), identity, sdd.WorkflowAdvanceRequest{Instance: serve.Instance, Report: map[string]any{"body": "one"}})
	if err != nil {
		t.Fatalf("the holder should continue past an append racing its own, got %v", err)
	}
	stored, err := sessions.Load(t.Context(), holder.ID())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored.Events[len(stored.Events)-1].Payload), `"body":"one"`) {
		t.Fatalf("the holder's report should land last, after the racing read: %s", stored.Events[len(stored.Events)-1].Payload)
	}
	assertIncreasing(t, workflowSeqs(t, stored))
}

// An instance another attach started in a dependency folds with the project it
// targets, as replay restores it.
func TestWorkflowHolderFoldsAnotherAttachsStartedInstance(t *testing.T) {
	application, alice := newClosureFixture(t)
	holder, _, err := application.OpenWorkflow(t.Context(), alice, "project-a", sdd.WorkflowOpenRequest{ClientName: "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	other := attachAnother(t, application, alice, holder.ID())
	started, err := other.Start(t.Context(), alice, sdd.WorkflowStartRequest{Canonical: "capture", Project: "project-b"})
	if err != nil {
		t.Fatal(err)
	}
	holder.ServeText("lane step", servedLane, nil)
	err = holder.RecordAnchors(t.Context(), alice)
	if err != nil {
		t.Fatalf("the holder should continue past another attach's start, got %v", err)
	}
	position, err := holder.ServeAll(t.Context(), alice)
	if err != nil {
		t.Fatal(err)
	}
	var project sdd.ProjectID
	for _, open := range position.Open {
		if open.Instance == started.Instance {
			project = open.Project
		}
	}
	if project != "project-b" {
		t.Fatalf("the holder should serve the folded instance in the project it targets, got %q", project)
	}
}
