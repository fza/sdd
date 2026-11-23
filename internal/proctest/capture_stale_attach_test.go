package proctest_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// A stale attach reporting into another instance while another attach's
// operation is unfinished is refused as a fresh attach would be, and its report
// does not land in the middle of that write.
func TestCapture_AStaleAttachCannotReportDuringAnotherAttachsUnfinishedWrite(t *testing.T) {
	world, session := newCaptureWorld(t, "stale-report", proctest.WithFinalizers(&captureCompletionFailure{}))
	first := session.Start(t, "capture", nil)
	second := session.Start(t, "capture", nil)
	stager, _ := world.Resume(t, session.ID, "stale-report-stager")
	stager.Start(t, "capture", nil)
	other, _ := world.Resume(t, session.ID, "stale-report-other")
	other.Report(t, first.Instance, captureDraft())
	failed, err := other.AnswerErr(t, first.Instance, "playback", "confirm", nil, "publish it")
	if err != nil {
		t.Fatal(err)
	}
	if failed.PendingOperation == nil {
		t.Fatalf("the failed completion should leave the write pending: %+v", failed)
	}

	_, err = session.ReportErr(t, second.Instance, captureDraft())
	want := fmt.Sprintf("operation %q is unfinished; only its retry or cancellation is accepted", failed.PendingOperation.Command)
	var refused *sdd.ApplicationError
	if !errors.As(err, &refused) || refused.Code != sdd.ErrorOperationPending || refused.Error() != want {
		t.Fatalf("the stale attach's report should be refused with %q, got %v", want, err)
	}
	stored, err := world.Sessions.Load(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	opened := false
	for _, item := range stored.Events {
		if item.Code != sdd.WorkflowEventCode {
			continue
		}
		var event engine.Event
		if err := json.Unmarshal(item.Payload, &event); err != nil {
			t.Fatal(err)
		}
		opened = opened || event.Event == engine.EventMutationIntent
		if opened && event.Event == engine.EventReport {
			t.Errorf("a report landed after the unfinished operation opened: %s", item.Payload)
		}
	}
	if !opened {
		t.Fatal("the log should hold the other attach's unfinished operation")
	}
	stager.Stage(t, "notes.md", []byte("Notes."))
}
