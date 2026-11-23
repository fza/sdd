package proctest_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// In an assisted session the person is present: a check the agent spent its
// unattended answers on is put to them, and their relayed words answer it, as
// in a guided session. Auto mode keeps parking it for a resume in one.

// heldTimeout drives an assisted capture to pre-flight's timeout twice: the
// agent retries the first itself, and the second finds its answer spent.
func heldTimeout(t *testing.T, connID string) (*proctest.World, *proctest.Session, string, *sdd.WorkflowServe) {
	t.Helper()
	world, session := newCaptureWorld(t, connID, proctest.WithAssistedDialogue(), clientActor(connID))
	world.LLM.PreflightErr = preflightTimedOut()
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	proctest.RequireStep(t, session.Report(t, instance, captureDraft()), "preflightTimedOut")
	serve = session.Decide(t, instance, "preflightTimedOut", "retry", nil, "a slow call may pass")
	proctest.RequireStep(t, serve, "preflightTimedOut")
	return world, session, instance, serve
}

func TestAssisted_ASpentCheckIsPutToThePerson(t *testing.T) {
	_, _, _, serve := heldTimeout(t, "assisted-held-serve")
	if serve.Parked != nil || serve.PendingChooser == nil || serve.PendingChooser.Kind != "user" {
		t.Fatalf("the spent check should be put to the person, not parked for a resume, got parked %+v chooser %+v", serve.Parked, serve.PendingChooser)
	}
	if !strings.HasPrefix(serve.Goal, "put the choice to the person") {
		t.Errorf("the goal should have the agent ask the person, got %q", serve.Goal)
	}
	text := serve.Instructions
	for _, want := range []string{
		"Held at preflightTimedOut for the person: blocked again after 1 unattended retry.",
		"relay their answer verbatim as `userWords`",
		"Put it to the user in their language.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the held step should carry %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Answer **retry** with your reason") {
		t.Errorf("the held step should not address the agent's own answer:\n%s", text)
	}
	properties, _ := serve.ReportSchema["properties"].(map[string]any)
	choice, _ := properties["choice"].(map[string]any)
	required, _ := serve.ReportSchema["required"].([]string)
	if enum, _ := choice["enum"].([]any); len(enum) != 4 || properties["reason"] != nil || !slices.Contains(required, "userWords") {
		t.Errorf("the held step's schema should take the person's words for any option, got %v", serve.ReportSchema)
	}
}

func TestAssisted_ThePersonsRelayedAnswerSettlesASpentCheck(t *testing.T) {
	t.Run("directly", func(t *testing.T) {
		world, session, instance, _ := heldTimeout(t, "assisted-held-direct")
		world.LLM.PreflightErr = nil
		serve := session.Answer(t, instance, "preflightTimedOut", "retry", nil, "send it again")
		proctest.RequireStep(t, serve, "playback")
	})
	t.Run("after resume_session", func(t *testing.T) {
		world, session, instance, _ := heldTimeout(t, "assisted-held-resume")
		world.Restart(t)
		sessions, err := world.App.ListWorkflowSessions(t.Context(), world.Identity, "proctest")
		if err != nil {
			t.Fatal(err)
		}
		if parked := parkedMove(sessions, session.ID); parked == nil || parked.Step != "preflightTimedOut" {
			t.Fatalf("the held check should be listed as parked after a restart, got %+v", sessions)
		}
		resumed, served := world.Resume(t, session.ID, "assisted-held-resume")
		if serve := servedInstance(served.Open, instance); serve == nil || serve.Step != "preflightTimedOut" || serve.PendingChooser == nil {
			t.Fatalf("the resume should put the held check to the person, got %+v", serve)
		}
		world.LLM.PreflightErr = nil
		serve := resumed.Answer(t, instance, "preflightTimedOut", "retry", nil, "send it again")
		proctest.RequireStep(t, serve, "playback")
		sessions, err = world.App.ListWorkflowSessions(t.Context(), world.Identity, "proctest")
		if err != nil {
			t.Fatal(err)
		}
		if parked := parkedMove(sessions, session.ID); parked != nil && parked.Step == "preflightTimedOut" {
			t.Errorf("the person's answer should end the hold at the check, got %+v", parked)
		}
	})
}

func TestAssisted_TheAgentCannotAnswerASpentCheck(t *testing.T) {
	world, session, instance, _ := heldTimeout(t, "assisted-held-agent")
	_, err := session.DecideErr(t, instance, "preflightTimedOut", "retry", nil, "a slow call may pass")
	if err == nil || !strings.Contains(err.Error(), "preflightTimedOut is the person's now") || !strings.Contains(err.Error(), "as userWords") {
		t.Fatalf("the agent's own answer should be refused, naming how the person's answer is relayed, got %v", err)
	}
	_, err = session.WF.Abandon(t.Context(), world.Identity, instance, "the check stays open")
	if err == nil || !strings.Contains(err.Error(), "the person drops it by answering abort") {
		t.Fatalf("the agent should not abandon a check held for the person, got %v", err)
	}
}
