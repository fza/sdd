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
// the ledger keeps (d-cpt-aen): a tag one consumer
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
	err = w.RecordAnchors(t.Context(), identity)
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

// A text piece keys on its content without the tags placed in it: a lane
// carrying an entry, which takes a fresh tag each serve, still holds once
// proved.
func TestServeTextHoldsALaneWhoseCarriedTagChanged(t *testing.T) {
	application, _, _, _ := newStampWorkflowApp(t, "", "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	w, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp-a"})
	if err != nil {
		t.Fatal(err)
	}
	lane := func(tag string) string {
		return servedLane + " The overview follows: " + model.AnchorMark(tag) + " Entries record decisions."
	}
	proof := proofOf(t, w.ServeText("lane typeSystem", lane("ab12"), nil))
	err = w.RecordAnchors(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	err = w.BeginCall([]string{proof}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !w.ServeText("lane typeSystem", lane("cd34"), nil).Held {
		t.Error("a lane differing only in a carried entry's tag should serve as its pointer once proved")
	}
	if w.ServeText("lane typeSystem", lane("cd34")+" Changed.", nil).Held {
		t.Error("a lane whose words changed should serve in full")
	}
}
