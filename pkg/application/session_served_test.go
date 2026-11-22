package application_test

import (
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/test"
)

const servedLane = "Draft one entry with the user, dialogue first. Then check its grounding against the graph before you write it."

// answerTo answers the question lines from the texts an agent holds.
func answerTo(t *testing.T, line string, texts ...string) string {
	t.Helper()
	answer, ok := test.AnswerAll(line, texts)
	if !ok {
		t.Fatalf("the questions should be answerable from the served text, got %q", line)
	}
	return answer
}

// serveAndAsk serves the lane in full, records it and ends the response with
// a question, returning the right answer to it.
func serveAndAsk(t *testing.T, w *sdd.WorkflowSession, identity sdd.RequestIdentity) (string, error) {
	t.Helper()
	if w.ServeText("lane step", servedLane, nil).Held {
		t.Fatal("a first serve should send the lane in full")
	}
	err := w.RecordPieces(t.Context(), identity)
	if err != nil {
		return "", err
	}
	line, err := w.Question(t.Context(), identity)
	if err != nil {
		return "", err
	}
	return answerTo(t, line, servedLane), nil
}

// TestQuestionsSurviveReplayAndReorientServesInFull covers the served memory
// the ledger keeps (d-cpt-aen): a question one consumer was asked is checked
// for the next one that loads the handle, and a reorientation serves its
// call in full without closing the question.
func TestQuestionsSurviveReplayAndReorientServesInFull(t *testing.T) {
	application, _, _, _ := newStampWorkflowApp(t, "", "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	w, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp-a"})
	if err != nil {
		t.Fatal(err)
	}
	if w.ServeText("lane step", servedLane, nil).Held {
		t.Fatal("a first serve should send the lane in full")
	}
	err = w.RecordPieces(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	line, err := w.Question(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	answer := answerTo(t, line, servedLane)
	holds := func(t *testing.T, session *sdd.WorkflowSession, want bool) {
		t.Helper()
		session.BeginCall(answer, nil)
		if got := session.ServeText("lane step", servedLane, nil).Held; got != want {
			t.Fatalf("a call carrying the right answer should hold the lane: %v, want %v", got, want)
		}
	}

	replayed, err := application.LoadWorkflow(t.Context(), identity, sdd.WorkflowResumeRequest{SessionID: w.ID(), ClientName: "mcp-b"})
	if err != nil {
		t.Fatal(err)
	}
	holds(t, replayed, true)

	replayed.BeginCall(answer, nil)
	err = replayed.Reorient(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ServeText("lane step", servedLane, nil).Held {
		t.Fatal("a reorientation should serve its call in full")
	}

	again, _, err := application.ResumeWorkflow(t.Context(), identity, sdd.WorkflowResumeRequest{SessionID: w.ID(), ClientName: "mcp-c"})
	if err != nil {
		t.Fatal(err)
	}
	holds(t, again, true)
}
