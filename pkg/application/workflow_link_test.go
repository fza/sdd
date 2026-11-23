package application_test

import (
	"strings"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// handleLane is a lane naming the session that serves it, as a text that
// differs between sessions only by the handle.
const handleLane = "Resume this dialogue later with resume_session HANDLE when its topic returns. Its parked moves keep their state, so going back costs less than reading the graph afresh."

func withHandle(w *sdd.WorkflowSession) string {
	return strings.ReplaceAll(handleLane, "HANDLE", string(w.ID()))
}

// linkedPair opens a session that serves handleLane and a second session
// linking it, returning the second with the right answer to its opening
// question.
func linkedPair(t *testing.T, application *sdd.Application, identity sdd.RequestIdentity) (second *sdd.WorkflowSession, right string) {
	t.Helper()
	first, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp-a"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ServeText("lane resume", withHandle(first), nil).Held {
		t.Fatal("a first serve should send the lane in full")
	}
	err = first.RecordPieces(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err = application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp-a", Link: first.ID()})
	if err != nil {
		t.Fatalf("opening a session linking an earlier one: %v", err)
	}
	if second.LinkNotice() != "" {
		t.Fatalf("a link to a session with served texts should be used, got %q", second.LinkNotice())
	}
	line, err := second.Question(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "q lane resume:") {
		t.Fatalf("the linked session's opening should ask about the carried text, got %q", line)
	}
	return second, answerTo(t, line, withHandle(first))
}

func TestALinkCarriesHeldTextsMatchedWithoutTheHandle(t *testing.T) {
	application, _, _, _ := newStampWorkflowApp(t, "", "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	second, right := linkedPair(t, application, identity)

	replayed, err := application.RefreshWorkflow(t.Context(), identity, second.ID())
	if err != nil {
		t.Fatal(err)
	}
	replayed.BeginCall(right, nil)
	if !replayed.ServeText("lane resume", withHandle(replayed), nil).Held {
		t.Error("after a right answer a text identical but for the handle should serve short")
	}
}

func TestALinkedPieceServesInFullOnRereadAndResume(t *testing.T) {
	application, _, _, _ := newStampWorkflowApp(t, "", "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	second, right := linkedPair(t, application, identity)

	second.BeginCall(right, []string{"lane resume"})
	if second.ServeText("lane resume", withHandle(second), nil).Held {
		t.Error("a piece pointed at through the link should serve in full when the call rereads it")
	}

	resumed, _, err := application.ResumeWorkflow(t.Context(), identity, sdd.WorkflowResumeRequest{SessionID: second.ID(), ClientName: "mcp-a"})
	if err != nil {
		t.Fatal(err)
	}
	resumed.BeginCall(right, nil)
	err = resumed.Reorient(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ServeText("lane resume", withHandle(resumed), nil).Held {
		t.Error("a resume should serve its call in full whatever the link")
	}
}

func TestAnUnknownLinkIsRefused(t *testing.T) {
	application, _, _, _ := newStampWorkflowApp(t, "", "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	_, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp-a", Link: "s_20260101-000000-00000000000000000000000000000000"})
	if err == nil {
		t.Fatal("a link to a session the store does not hold should be refused")
	}
	for _, want := range []string{"unknown or was removed", "start_session works without link"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal should say the session is unknown or removed and that the door works without the link, got %q", err)
		}
	}
}

func TestALinkToALogWithoutServedTextsGoesUnused(t *testing.T) {
	application, _, _, _ := newStampWorkflowApp(t, "", "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	earlier, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp-a"})
	if err != nil {
		t.Fatal(err)
	}
	linked, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp-a", Link: earlier.ID()})
	if err != nil {
		t.Fatalf("a link to a log without served texts should still open the session: %v", err)
	}
	notice := linked.LinkNotice()
	if !strings.Contains(notice, "not used") || !strings.Contains(notice, "no record of served texts") || strings.Contains(notice, "\n") {
		t.Errorf("the opening should say in one line that the link was not used and why, got %q", notice)
	}
	if line, err := linked.Question(t.Context(), identity); err != nil || line != "" {
		t.Errorf("an unused link should carry no text to ask about, got %q (%v)", line, err)
	}
}

// A delegate starts with a fresh context, so its child session links
// nothing: a text its dispatching session carries serves the child in full.
func TestADelegateChildSessionCarriesNoLink(t *testing.T) {
	f := newDelegateApplication(t, nil)
	parent, right := linkedPair(t, f.app, f.identity)
	f.parent = parent.ID()
	serve, err := parent.Start(t.Context(), f.identity, sdd.WorkflowStartRequest{Canonical: "dispatcher"})
	if err != nil {
		t.Fatal(err)
	}
	f.dispatch = serve.Instance
	_, dispatch := f.fork(t)

	child := f.session(t, dispatch.Session)
	child.BeginCall(right, nil)
	if child.ServeText("lane resume", withHandle(child), nil).Held {
		t.Error("a delegate's child session should get every text in full")
	}
	holder := f.session(t, f.parent)
	holder.BeginCall(right, nil)
	if !holder.ServeText("lane resume", withHandle(holder), nil).Held {
		t.Error("the linked dispatching session should still carry the text")
	}
}
