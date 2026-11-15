package proctest_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/engine"
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
// session's other staged attachments when the session ends.
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
			replayed, _ := world.Resume(t, session.ID, "staged-cleanup-replayed")
			if handles := replayed.WF.StagedHandles(); !slices.Equal(handles, []string{"notes.md"}) {
				t.Errorf("a replayed session should name only the unpublished attachment as staged, got %v", handles)
			}
			if _, _, err := replayed.WF.ReadStagedAttachment(t.Context(), world.Identity, "notes.md", 0, 1024); err != nil {
				t.Errorf("an attachment no entry used should stay staged: %v", err)
			}

			scenario.end(t, world, replayed)
			if got := stagedBlobs(t, world, replayed); got != 0 {
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

// A capture naming an attachment a publication released refuses it as no
// longer staged rather than writing from the deleted file.
func TestCapture_ACaptureNamingAReleasedAttachmentRefusesIt(t *testing.T) {
	world, session := newCaptureWorld(t, "staged-released")
	handle := session.Stage(t, "record.md", []byte("The record."))
	first := session.Start(t, "capture", nil)
	draft := captureDraft()
	draft["attachments"] = []any{handle}
	session.Report(t, first.Instance, draft)
	serve := session.Answer(t, first.Instance, "playback", "confirm", nil, "capture it")
	proctest.RequireStep(t, serve, "verifySummary")
	session.Answer(t, first.Instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	if got := stagedBlobs(t, world, session); got != 0 {
		t.Fatalf("the publication should release the attachment it used, %d staged", got)
	}

	second := session.Start(t, "capture", nil)
	_, err := session.ReportErr(t, second.Instance, draft)
	if err == nil {
		_, err = session.AnswerErr(t, second.Instance, "playback", "confirm", nil, "capture it")
	}
	if err == nil || !strings.Contains(err.Error(), `attachment "record.md" has not been staged`) {
		t.Errorf("the capture should refuse the released attachment as no longer staged, got %v", err)
	}
}

// duringPublication runs once inside the first publication, after its write
// was recorded and before its staged attachments are released.
type duringPublication struct{ run func() }

func (*duringPublication) Name() string { return "during-publication" }
func (d *duringPublication) Finalize(context.Context, sdd.AppliedMutation) error {
	if run := d.run; run != nil {
		d.run = nil
		run()
	}
	return nil
}

// appendReport logs a report of fields into instance at step, as another
// attach of the same session would.
func appendReport(t *testing.T, world *proctest.World, id sdd.SessionID, instance, step string, fields map[string]any) {
	t.Helper()
	stored, err := world.Sessions.Load(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	seq := 0
	for _, item := range stored.Events {
		if item.Code != sdd.WorkflowEventCode {
			continue
		}
		var event engine.Event
		if err := json.Unmarshal(item.Payload, &event); err != nil {
			t.Fatal(err)
		}
		seq = max(seq, event.Seq)
	}
	payload, err := json.Marshal(engine.Event{
		V: engine.LogVersion, TS: time.Now().UTC(), Session: string(id), Seq: seq + 1,
		Instance: instance, Event: engine.EventReport, Data: map[string]any{"step": step, "fields": fields},
	})
	if err != nil {
		t.Fatal(err)
	}
	report := sdd.StoredEvent{CodecVersion: sdd.SessionCodecVersion, Code: sdd.WorkflowEventCode, Payload: payload}
	if _, err := world.Sessions.Append(t.Context(), id, stored.Version, sdd.SessionAppend{Metadata: &stored.Metadata, Events: []sdd.StoredEvent{report}}); err != nil {
		t.Fatal(err)
	}
}

// A report naming a staged attachment that lands from another attach while a
// capture is published makes the publication's release a session conflict:
// its claims were computed without that report, so nothing is deleted. A
// retry from a fresh attach reports the written entry, frees the files no
// capture claims and keeps the one the report named.
func TestCapture_PublicationKeepsAnAttachmentAReportNamedMeanwhile(t *testing.T) {
	race := &duringPublication{}
	world, session := newCaptureWorld(t, "staged-race", proctest.WithFinalizers(race))
	handle := session.Stage(t, "record.md", []byte("The record."))
	extra := session.Stage(t, "extra.md", []byte("Only the first capture's."))
	first := session.Start(t, "capture", nil)
	second := session.Start(t, "capture", nil)
	draft := captureDraft()
	draft["attachments"] = []any{handle, extra}
	session.Report(t, first.Instance, draft)
	race.run = func() {
		appendReport(t, world, session.ID, second.Instance, second.Step, map[string]any{"attachments": []any{handle}})
	}
	failed, err := session.AnswerErr(t, first.Instance, "playback", "confirm", nil, "capture it")
	if race.run != nil {
		t.Fatal("the report should land during the publication")
	}
	refusal := ""
	if err != nil {
		refusal = err.Error()
	} else if failed.PendingOperation != nil {
		refusal = failed.PendingOperation.Error
	}
	if !strings.Contains(refusal, "is written: freeing its staged attachments") || !strings.Contains(refusal, "reorient with resume_session") {
		t.Fatalf("the release should be refused as a session conflict after the write, got %q", refusal)
	}
	if got := stagedBlobs(t, world, session); got != 2 {
		t.Fatalf("a refused release should delete nothing, %d staged", got)
	}

	other, resumed := world.Resume(t, session.ID, "staged-race-other")
	if resumed.PendingOperation == nil {
		t.Fatal("the publication should stay pending after its refused release")
	}
	retried, err := other.WF.Advance(t.Context(), world.Identity, sdd.WorkflowAdvanceRequest{Instance: first.Instance, RetryRef: resumed.PendingOperation.RetryRef})
	if err != nil {
		t.Fatal(err)
	}
	proctest.RequireStep(t, retried, "verifySummary")
	published := other.Answer(t, first.Instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	if written, _ := published.Produced["entryId"].(string); written == "" || written != resumed.PendingOperation.Values["entryId"] {
		t.Fatalf("the retry should report the entry the refused release followed, got %q", written)
	}
	if got := stagedBlobs(t, world, other); got != 1 {
		t.Errorf("the retry should free the attachment no capture claims and keep the claimed one, %d staged", got)
	}
	if _, _, err := other.WF.ReadStagedAttachment(t.Context(), world.Identity, "record.md", 0, 1024); err != nil {
		t.Errorf("the attachment the report named should stay staged: %v", err)
	}

	other.Report(t, second.Instance, captureDraft())
	serve := other.Answer(t, second.Instance, "playback", "confirm", nil, "capture it")
	proctest.RequireStep(t, serve, "verifySummary")
	serve = other.Answer(t, second.Instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	id, _ := serve.Produced["entryId"].(string)
	attachment, err := world.App.ReadAttachment(t.Context(), world.Identity, "proctest", sdd.ReadAttachmentRequest{EntryID: id, Filename: "record.md", MaxBytes: 1024})
	if err != nil || string(attachment.Page.Content) != "The record." {
		t.Fatalf("the second capture should publish the attachment it named: %+v, %v", attachment, err)
	}
}
