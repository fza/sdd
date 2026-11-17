package proctest_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/llm"
)

// An autonomous capture with the verifier on runs pre-flight before playback,
// so a pre-flight parked there has no confirmation behind it. A person taking
// it over in the guided mode gets the playback first.
func TestCapture_AGuidedRetryOfAnUnconfirmedParkedPreflightReopensPlayback(t *testing.T) {
	world, session := newCaptureWorld(t, "mode-switch-unconfirmed", proctest.WithAutonomousDialogue(), clientActor("mode-switch-unconfirmed"),
		proctest.WithVerifier(sdd.VerifierPolicy{Mode: "prose"}, 200000, 4096))
	world.LLM.PreflightErr = preflightTimedOut()
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "preflightTimedOut")
	serve = session.Decide(t, instance, "preflightTimedOut", "retry", nil, "a slow call may pass")
	if serve.Parked == nil || serve.Parked.Step != "preflightTimedOut" {
		t.Fatalf("a second timeout should park the capture, got step %s parked %+v", serve.Step, serve.Parked)
	}

	world.SetDialogue("guided")
	resumed, _, err := world.App.ResumeWorkflow(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: session.ID, ClientName: "person"})
	if err != nil {
		t.Fatal(err)
	}
	session.WF = resumed
	world.LLM.PreflightErr = nil
	serve = session.Answer(t, instance, "preflightTimedOut", "retry", nil, "retry it")
	proctest.RequireStep(t, serve, "playback")
	if serve.PendingChooser == nil || serve.PendingChooser.Kind != "user" {
		t.Fatalf("the reopened playback should be the person's choice, got %+v", serve.PendingChooser)
	}

	serve = session.Answer(t, instance, "playback", "confirm", nil, "write it")
	proctest.RequireStep(t, serve, "verifySummary")
	if got := world.LLM.Calls(llm.PurposePreflight); got != 3 {
		t.Fatalf("pre-flight should judge the confirmed draft after its two timeouts, ran %d times", got)
	}
}
