package application_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// attachAsCLI resumes the session the way a CLI call with --session does.
func attachAsCLI(t *testing.T, application *sdd.Application, identity sdd.RequestIdentity, id sdd.SessionID) *sdd.WorkflowSession {
	t.Helper()
	cli, _, err := application.ResumeWorkflow(t.Context(), identity, sdd.WorkflowResumeRequest{SessionID: id, ClientName: "sdd-cli"})
	if err != nil {
		t.Fatal(err)
	}
	return cli
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

// A holder of a session, as the MCP serve holds one, continues after a CLI
// --session call appended to the stored log: it folds what the other attach
// wrote and lands its own event after it (20261109-055313-s-tac-7j7).
func TestWorkflowHolderFoldsAnotherAttachsAppends(t *testing.T) {
	application, sessions, _, _ := newStampWorkflowApp(t, "", "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	holder, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	serve, err := holder.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "waiting-test"})
	if err != nil {
		t.Fatal(err)
	}
	holder.ServeText("lane step", servedLane, nil)
	err = holder.RecordServed(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}

	cli := attachAsCLI(t, application, identity, holder.ID())
	err = cli.LogRead(t.Context(), identity, "show", []string{"20260713-060000-d-prc-trm"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = cli.Reorient(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cli.StageAttachment(t.Context(), identity, "notes.md", []byte("staged by the CLI\n"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = holder.Advance(t.Context(), identity, sdd.WorkflowAdvanceRequest{Instance: serve.Instance, Report: map[string]any{"body": "one"}})
	if err != nil {
		t.Fatalf("the holder should continue past another attach's appends, got %v", err)
	}
	if !reflect.DeepEqual(holder.StagedHandles(), cli.StagedHandles()) || len(holder.StagedHandles()) != 1 {
		t.Fatalf("the holder should hold the other attach's staged file, got %v want %v", holder.StagedHandles(), cli.StagedHandles())
	}
	stored, err := sessions.Load(t.Context(), holder.ID())
	if err != nil {
		t.Fatal(err)
	}
	if holder.Binding().Version != stored.Version {
		t.Fatalf("the holder should sit at the stored version %d, got %d", stored.Version, holder.Binding().Version)
	}
	assertIncreasing(t, workflowSeqs(t, stored))
	refreshed, err := application.RefreshWorkflow(t.Context(), identity, holder.ID())
	if err != nil {
		t.Fatal(err)
	}
	position, err := refreshed.ServeAll(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	var collected any
	for _, open := range position.Open {
		if open.Instance == serve.Instance {
			collected = open.Collected["body"]
		}
	}
	if collected != "one" {
		t.Fatalf("the replayed session should hold the holder's report, got %v", collected)
	}
}

// writeBetweenLoadAndAppend lets another attach append once, between a
// holder's load of the log and its append: the race of a CLI call landing
// while a serve is in flight.
type writeBetweenLoadAndAppend struct {
	sdd.SessionStore
	armed atomic.Bool
	write func(context.Context)
}

func (s *writeBetweenLoadAndAppend) Append(ctx context.Context, id sdd.SessionID, version uint64, data sdd.SessionAppend) (uint64, error) {
	if s.armed.CompareAndSwap(true, false) {
		s.write(ctx)
	}
	return s.SessionStore.Append(ctx, id, version, data)
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
		cli := attachAsCLI(t, application, identity, holder.ID())
		err := cli.LogRead(t.Context(), identity, "show", []string{"20260713-060000-d-prc-trm"}, nil)
		if err != nil {
			t.Error(err)
		}
	}
	racing.armed.Store(true)
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
	other := attachAsCLI(t, application, alice, holder.ID())
	started, err := other.Start(t.Context(), alice, sdd.WorkflowStartRequest{Canonical: "capture", Project: "project-b"})
	if err != nil {
		t.Fatal(err)
	}
	holder.ServeText("lane step", servedLane, nil)
	err = holder.RecordServed(t.Context(), alice)
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

// A holder's own non-engine append, a staged file, catches up the same way.
func TestWorkflowHolderStagesPastAnotherAttachsRead(t *testing.T) {
	application, _, _, _ := newStampWorkflowApp(t, "", "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	holder, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	cli := attachAsCLI(t, application, identity, holder.ID())
	err = cli.LogRead(t.Context(), identity, "show", []string{"20260713-060000-d-prc-trm"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = holder.StageAttachment(t.Context(), identity, "notes.md", []byte("staged by the holder\n"))
	if err != nil {
		t.Fatalf("the holder should stage past another attach's read, got %v", err)
	}
}
