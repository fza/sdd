package proctest_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// stagedBlobs counts the staged attachments the session holds on disk.
func stagedBlobs(t *testing.T, world *proctest.World, session *proctest.Session) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(world.StagedBlobDir, world.Identity.Subject, string(session.ID)))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// A write left pending keeps the staged attachment it claims until a retry
// publishes it.
func TestCapture_APendingWriteKeepsItsStagedAttachments(t *testing.T) {
	world, session := newCaptureWorld(t, "staged-pending", proctest.WithFinalizers(&captureCompletionFailure{}))
	serve := session.Start(t, "capture", nil)
	handle := session.Stage(t, "record.md", []byte("The record."))
	draft := captureDraft()
	draft["attachments"] = []any{handle}
	session.Report(t, serve.Instance, draft)
	failed, err := session.AnswerErr(t, serve.Instance, "playback", "confirm", nil, "publish it")
	if err != nil {
		t.Fatal(err)
	}
	pending := failed.PendingOperation
	if pending == nil {
		t.Fatalf("the failed completion should leave the write pending: %+v", failed)
	}
	if got := stagedBlobs(t, world, session); got != 1 {
		t.Errorf("the pending write's attachment should stay staged, %d staged", got)
	}
	if _, err := world.App.AbandonWorkflowSession(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: session.ID}, "leaving"); err == nil {
		t.Error("a session holding a pending write should refuse to end")
	}
	if got := stagedBlobs(t, world, session); got != 1 {
		t.Errorf("a refused end should leave the pending write's attachment staged, %d staged", got)
	}
	if _, err := session.WF.Advance(t.Context(), world.Identity, sdd.WorkflowAdvanceRequest{Instance: serve.Instance, RetryRef: pending.RetryRef}); err != nil {
		t.Fatal(err)
	}
	if got := stagedBlobs(t, world, session); got != 0 {
		t.Errorf("the retried write should free the attachment it published, %d staged", got)
	}
}

// A staged attachment goes once the entry using it is published, and the
// session's other staged attachments when the session ends
// (20261114-095140-d-cpt-ad4, rule 1).
func TestCapture_PublicationAndSessionEndFreeStagedAttachments(t *testing.T) {
	for _, scenario := range []struct {
		name string
		end  func(t *testing.T, world *proctest.World, session *proctest.Session)
	}{
		{name: "abandon", end: func(t *testing.T, world *proctest.World, session *proctest.Session) {
			if _, err := world.App.AbandonWorkflowSession(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: session.ID}, "done"); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "conclude", end: func(t *testing.T, world *proctest.World, session *proctest.Session) {
			shell, err := session.WF.ServeShell(t.Context(), world.Identity)
			if err != nil {
				t.Fatal(err)
			}
			serve := session.Answer(t, shell.Instance, "junction", "conclude", nil, "that is all")
			proctest.RequireStatus(t, serve, "completed")
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			world, session := newCaptureWorld(t, "staged-cleanup")
			handle := session.Stage(t, "record.md", []byte("The record."))
			session.Stage(t, "notes.md", []byte("Notes kept for later."))
			serve := session.Start(t, "capture", nil)
			draft := captureDraft()
			draft["attachments"] = []any{handle}
			draft["body"] = "The observation has a [full record]({{attachments}}/record.md)."
			session.Report(t, serve.Instance, draft)
			serve = session.Answer(t, serve.Instance, "playback", "confirm", nil, "capture it")
			proctest.RequireStep(t, serve, "verifySummary")
			serve = session.Answer(t, serve.Instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
			id, _ := serve.Produced["entryId"].(string)
			if _, err := world.App.ReadAttachment(t.Context(), world.Identity, "proctest", sdd.ReadAttachmentRequest{EntryID: id, Filename: "record.md", MaxBytes: 1024}); err != nil {
				t.Fatalf("the published attachment should read from the graph: %v", err)
			}
			if got := stagedBlobs(t, world, session); got != 1 {
				t.Errorf("publication should free the attachment it used and keep the other, %d staged left", got)
			}
			if _, _, err := session.WF.ReadStagedAttachment(t.Context(), world.Identity, "record.md", 0, 1024); err == nil {
				t.Error("the published attachment's handle should no longer name a staged file")
			}
			session.Refresh(t)
			if handles := session.WF.StagedHandles(); !slices.Equal(handles, []string{"notes.md"}) {
				t.Errorf("a replayed session should name only the unpublished attachment as staged, got %v", handles)
			}
			if _, _, err := session.WF.ReadStagedAttachment(t.Context(), world.Identity, "notes.md", 0, 1024); err != nil {
				t.Errorf("an attachment no entry used should stay staged: %v", err)
			}

			scenario.end(t, world, session)
			if got := stagedBlobs(t, world, session); got != 0 {
				t.Errorf("ending the session should free its staged attachments, %d left", got)
			}
		})
	}
}

// A staged attachment two running captures name stays staged until the second
// one is published.
func TestCapture_AnAttachmentAnotherCaptureNamesStaysStaged(t *testing.T) {
	world, session := newCaptureWorld(t, "staged-shared")
	handle := session.Stage(t, "record.md", []byte("The record."))
	first := session.Start(t, "capture", nil)
	second := session.Start(t, "capture", nil)
	for _, serve := range []*sdd.WorkflowServe{first, second} {
		draft := captureDraft()
		draft["attachments"] = []any{handle}
		session.Report(t, serve.Instance, draft)
	}
	publish := func(instance string) {
		t.Helper()
		serve := session.Answer(t, instance, "playback", "confirm", nil, "capture it")
		proctest.RequireStep(t, serve, "verifySummary")
		session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	}
	publish(first.Instance)
	if got := stagedBlobs(t, world, session); got != 1 {
		t.Errorf("an attachment another running capture names should stay staged, %d staged", got)
	}
	publish(second.Instance)
	if got := stagedBlobs(t, world, session); got != 0 {
		t.Errorf("the last capture naming the attachment should free it once published, %d staged", got)
	}
}
