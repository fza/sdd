package application_test

import (
	"context"
	"io"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
)

// The single-file staging signatures of upstream main stay callable, and a
// store without StagedBlobDeleter still is a StagedBlobStore.
var (
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
}

func TestSingleFileStagingStagesEmptyContent(t *testing.T) {
	application, _, _, _ := newStampWorkflowApp(t, "", "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	workflow, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{"nil.md": nil, "empty.md": {}} {
		handle, err := workflow.StageAttachment(t.Context(), identity, name, content)
		if err != nil {
			t.Fatalf("%s: empty content should stage an empty file: %v", name, err)
		}
		page, _, err := workflow.ReadStagedAttachment(t.Context(), identity, handle, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Content) != 0 {
			t.Fatalf("%s: staged content = %q, want empty", name, page.Content)
		}
	}
}
