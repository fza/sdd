package application_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
)

// The signatures programs built on the upstream packages call; a change to
// one fails this file's compilation.
var (
	_ func(*sdd.WorkflowServe) string                                                                   = (*sdd.WorkflowServe).ReminderInstructions
	_ func(*sdd.WorkflowSession, string) bool                                                           = (*sdd.WorkflowSession).ServedBefore
	_ func(*sdd.WorkflowSession, context.Context, sdd.RequestIdentity, []string) error                  = (*sdd.WorkflowSession).RecordServed
	_ func(*sdd.WorkflowSession, context.Context, sdd.RequestIdentity, string, []byte) (string, error)  = (*sdd.WorkflowSession).StageAttachment
	_ func(*sdd.WorkflowSession, context.Context, sdd.RequestIdentity, string, []types.PatchPair) error = (*sdd.WorkflowSession).EditStagedAttachment
	_ sdd.StagedBlobStore                                                                               = stagingOnlyBlobStore{}
)

// stagingOnlyBlobStore implements StagedBlobStore without StagedBlobDeleter.
type stagingOnlyBlobStore struct{}

func (stagingOnlyBlobStore) Stage(context.Context, sdd.SessionRef, string, io.Reader) (sdd.StagedBlob, error) {
	return sdd.StagedBlob{}, nil
}

func (stagingOnlyBlobStore) Open(context.Context, sdd.SessionRef, string) (io.ReadCloser, error) {
	return nil, nil
}

func (stagingOnlyBlobStore) DeleteStaged(context.Context, sdd.SessionRef) error { return nil }

func TestSingleFileStagingStagesAndEdits(t *testing.T) {
	application, _, _, _ := newStampWorkflowApp(t, "", "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	workflow, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp"})
	if err != nil {
		t.Fatal(err)
	}

	handle, err := workflow.StageAttachment(t.Context(), identity, "notes.md", []byte("first line"))
	if err != nil {
		t.Fatal(err)
	}
	if handle != "notes.md" {
		t.Fatalf("handle = %q, want the filename", handle)
	}
	err = workflow.EditStagedAttachment(t.Context(), identity, handle, []types.PatchPair{{Old: "first", New: "edited"}})
	if err != nil {
		t.Fatal(err)
	}
	page, _, err := workflow.ReadStagedAttachment(t.Context(), identity, handle, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(page.Content) != "edited line" {
		t.Fatalf("staged content = %q, want the edited text", page.Content)
	}

	if workflow.ServedBefore("any-hash") {
		t.Fatal("ServedBefore should report no hash as served")
	}
	err = workflow.RecordServed(t.Context(), identity, []string{"any-hash"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReadResultsCarryRenderedText(t *testing.T) {
	snapshot := acquiredSnapshot(t, "base", "r1", "Rendered body")
	app := preparationApp(t, acquiredRuntime(t, "base", staticGraphStore{snapshot: snapshot}), nil, nil)
	identity := sdd.RequestIdentity{Subject: "reader"}
	const id = "20260101-100000-s-tac-aaa"

	view, err := app.View(t.Context(), identity, "base", sdd.ViewRequest{Layout: "as-list"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view.Sections, id) {
		t.Fatalf("view text should list the entry, got %q", view.Sections)
	}
	show, err := app.Show(t.Context(), identity, "base", sdd.ShowRequest{IDs: []string{id}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(show.Entries, "Rendered body") {
		t.Fatalf("show text should carry the body, got %q", show.Entries)
	}
	search, err := app.Search(t.Context(), identity, "base", sdd.SearchRequest{SyncMode: sdd.SearchSyncNone, Terms: []string{"Rendered"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(search.Results, id) {
		t.Fatalf("search text should list the hit, got %q", search.Results)
	}
}
