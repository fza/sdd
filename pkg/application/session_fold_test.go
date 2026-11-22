package application_test

import (
	"context"
	"encoding/json"
	"errors"
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
// wrote and lands its own event after it.
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
	err = holder.RecordPieces(t.Context(), identity)
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
	_, err = cli.StageAttachments(t.Context(), identity, []sdd.StageItem{{Name: "notes.md", Content: []byte("staged by the CLI\n")}})
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

// writeBetweenLoadAndAppend lets another writer append between a holder's
// load of the log and its append, once ahead of each of the holder's next
// owed appends: the race of a CLI call landing while a serve is in flight.
// write gets how many are still owed after it.
type writeBetweenLoadAndAppend struct {
	sdd.SessionStore
	owed    atomic.Int32
	writing atomic.Bool
	write   func(ctx context.Context, owed int)
}

func (s *writeBetweenLoadAndAppend) Append(ctx context.Context, id sdd.SessionID, version uint64, data sdd.SessionAppend) (uint64, error) {
	if s.owed.Load() > 0 && s.writing.CompareAndSwap(false, true) {
		s.write(ctx, int(s.owed.Add(-1)))
		s.writing.Store(false)
	}
	return s.SessionStore.Append(ctx, id, version, data)
}

// appendRankingRecord appends a ranking record of another instance to the
// stored log, an event no instance of the holder depends on.
func appendRankingRecord(t *testing.T, sessions sdd.SessionStore, id sdd.SessionID) {
	t.Helper()
	stored, err := sessions.Load(t.Context(), id)
	if err != nil {
		t.Error(err)
		return
	}
	_, err = sessions.Append(t.Context(), id, stored.Version, sdd.SessionAppend{Events: []sdd.StoredEvent{{
		CodecVersion: sdd.SessionCodecVersion, Code: "workflow_ranking_scores", Payload: json.RawMessage(`{"instance":"i_99","prefilter":{}}`),
	}}})
	if err != nil {
		t.Error(err)
	}
}

// sessionCatchUps is how often one append of a holder catches up with other
// writers before it surfaces the conflict.
const sessionCatchUps = 8

// racedHolder opens a holder on the waiting procedure over a store that lets
// write land ahead of the holder's appends.
func racedHolder(t *testing.T) (*sdd.Application, *countingSessionStore, *writeBetweenLoadAndAppend, *sdd.WorkflowSession, string) {
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
	return application, sessions, racing, holder, serve.Instance
}

// A holder's append that meets another writer's append on every try catches
// up each time and lands, as long as none of them touches its instance.
func TestWorkflowHolderCatchesUpWithEachAppendRacingItsOwn(t *testing.T) {
	_, sessions, racing, holder, instance := racedHolder(t)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	racing.write = func(context.Context, int) { appendRankingRecord(t, sessions, holder.ID()) }
	racing.owed.Store(sessionCatchUps)
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
	if holder.Binding().Version != stored.Version {
		t.Fatalf("the holder should sit at the stored version %d, got %d", stored.Version, holder.Binding().Version)
	}
	assertIncreasing(t, workflowSeqs(t, stored))
}

// A writer that lands ahead of every try does not hold the holder's call
// forever: past its catch-ups the append surfaces the conflict.
func TestWorkflowHolderSurfacesAConflictPastItsCatchUps(t *testing.T) {
	_, sessions, racing, holder, instance := racedHolder(t)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	racing.write = func(context.Context, int) { appendRankingRecord(t, sessions, holder.ID()) }
	racing.owed.Store(sessionCatchUps + 1)
	_, err := holder.Advance(t.Context(), identity, sdd.WorkflowAdvanceRequest{Instance: instance, Report: map[string]any{"body": "one"}})
	var conflict *sdd.ApplicationError
	if !errors.As(err, &conflict) || conflict.Code != sdd.ErrorSessionConflict || !strings.Contains(err.Error(), "session version changed") {
		t.Fatalf("the append should surface the session conflict once its catch-ups are spent, got %v", err)
	}
	if left := racing.owed.Load(); left != 0 {
		t.Fatalf("the holder should have tried once and again after each catch-up, %d tries short", left)
	}
}

// A racing append that changes the instance the holder's event belongs to
// stays a conflict, also behind one the holder caught up with.
func TestWorkflowHolderRefusesARacingChangeToItsInstance(t *testing.T) {
	application, sessions, racing, holder, instance := racedHolder(t)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	racing.write = func(_ context.Context, owed int) {
		if owed > 0 {
			appendRankingRecord(t, sessions, holder.ID())
			return
		}
		cli := attachAsCLI(t, application, identity, holder.ID())
		_, err := cli.Advance(t.Context(), identity, sdd.WorkflowAdvanceRequest{Instance: instance, Report: map[string]any{"body": "another"}})
		if err != nil {
			t.Error(err)
		}
	}
	racing.owed.Store(2)
	_, err := holder.Advance(t.Context(), identity, sdd.WorkflowAdvanceRequest{Instance: instance, Report: map[string]any{"body": "one"}})
	var conflict *sdd.ApplicationError
	if !errors.As(err, &conflict) || conflict.Code != sdd.ErrorSessionConflict || !strings.Contains(err.Error(), "instance "+instance+" changed in another attach") {
		t.Fatalf("a change to the holder's instance should stay a conflict, got %v", err)
	}
}

func TestWorkflowHolderFoldsAnAppendRacingItsOwn(t *testing.T) {
	application, sessions, racing, holder, instance := racedHolder(t)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	racing.write = func(context.Context, int) {
		cli := attachAsCLI(t, application, identity, holder.ID())
		err := cli.LogRead(t.Context(), identity, "show", []string{"20260713-060000-d-prc-trm"}, nil)
		if err != nil {
			t.Error(err)
		}
	}
	racing.owed.Store(1)
	_, err := holder.Advance(t.Context(), identity, sdd.WorkflowAdvanceRequest{Instance: instance, Report: map[string]any{"body": "one"}})
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
	application, alice := newDependencyFixture(t)
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
	err = holder.RecordPieces(t.Context(), alice)
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
	_, err = holder.StageAttachments(t.Context(), identity, []sdd.StageItem{{Name: "notes.md", Content: []byte("staged by the holder\n")}})
	if err != nil {
		t.Fatalf("the holder should stage past another attach's read, got %v", err)
	}
}
