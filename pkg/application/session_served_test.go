package application_test

import (
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

const servedLane = "Draft one entry, dialogue first, then check its grounding."

// proofOf is the proof an agent holding a piece served in full would list.
func proofOf(t *testing.T, served sdd.ServedPiece) string {
	t.Helper()
	word, ok := model.WordAfterAnchor(served.Text, served.Tag)
	if served.Held || !ok {
		t.Fatalf("the piece should serve in full with its tag, got %+v", served)
	}
	return served.Tag + ":" + word
}

// TestAnchorsSurviveReplayAndReorientServesInFull covers the served memory
// the ledger keeps (d-cpt-aen, 20261112-075604-d-tac-t4r): a tag one consumer
// received is checked for the next one that loads the handle, and a
// reorientation serves its call in full without forgetting the tag.
func TestAnchorsSurviveReplayAndReorientServesInFull(t *testing.T) {
	application, _, _, _ := newStampWorkflowApp(t, "", "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	w, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp-a"})
	if err != nil {
		t.Fatal(err)
	}
	proof := proofOf(t, w.ServeText("lane step", servedLane, nil))
	err = w.RecordServed(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	holds := func(t *testing.T, session *sdd.WorkflowSession, want bool) {
		t.Helper()
		err := session.BeginCall([]string{proof}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := session.ServeText("lane step", servedLane, nil).Held; got != want {
			t.Fatalf("a call carrying the proof should hold the lane: %v, want %v", got, want)
		}
	}

	replayed, err := application.LoadWorkflow(t.Context(), identity, sdd.WorkflowResumeRequest{SessionID: w.ID(), ClientName: "mcp-b"})
	if err != nil {
		t.Fatal(err)
	}
	holds(t, replayed, true)

	err = replayed.BeginCall([]string{proof}, nil)
	if err != nil {
		t.Fatal(err)
	}
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

func TestBeginCallRefusesAMalformedProof(t *testing.T) {
	application, _, _, _ := newStampWorkflowApp(t, "", "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	w, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp-a"})
	if err != nil {
		t.Fatal(err)
	}
	err = w.BeginCall([]string{"no-word-here"}, nil)
	if err == nil {
		t.Error("a held item that is not tag:word should be refused")
	}
}
