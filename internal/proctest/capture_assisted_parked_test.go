package proctest_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/test"
)

// In an assisted session the person is present: a check the agent spent its
// unattended answers on is put to them, and their relayed words answer it, as
// in a guided session. Auto mode keeps parking it for a resume in one.

const heldBody = "A tactical gap: the fixture observes something in scope."

func ownerPoint(quote string) []any {
	return []any{map[string]any{
		"verdict": "whether this gap is in scope is the owner's call", "axis": "aspiration", "severity": "medium", "owner": true,
		"pointers": []any{map[string]any{"field": "body", "quote": quote}},
	}}
}

// heldDeepUser drives an assisted capture to deepUser twice: the agent revises
// the first open point itself, the person's adjust sends the draft back to the
// verifier, and the second open point finds the agent's answer spent.
func heldDeepUser(t *testing.T, connID string) (*proctest.World, *proctest.Session, *deepExchange, *sdd.WorkflowServe) {
	t.Helper()
	world, session := newDeepWorld(t, connID, sdd.VerifierPolicy{}, proctest.WithAssistedDialogue(), clientActor(connID))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	proctest.RequireStep(t, session.Report(t, instance, captureDraft()), "deepDispatch")
	x := dispatchDeep(t, world, session, instance)
	x.judge(t, ownerPoint("observes something"))
	proctest.RequireStep(t, x.pickUp(t), "deepAnswer")
	session.Answer(t, instance, "deepAnswer", "accept", nil, "")
	proctest.RequireStep(t, session.Answer(t, instance, "deepProposal", "escalate", nil, ""), "deepUser")
	proctest.RequireStep(t, session.Decide(t, instance, "deepUser", "revise", map[string]any{"body": heldBody}, "fold in the point"), "playback")
	session.Answer(t, instance, "playback", "adjust", map[string]any{"body": heldBody + " Twice."}, "say it happens twice")
	x.pickUpRevision(t)
	x.delegate.Answer(t, x.child, "read", "judge", map[string]any{"findings": ownerPoint("Twice.")}, "")
	proctest.RequireStep(t, x.pickUpFindings(t), "deepAnswer")
	session.Answer(t, instance, "deepAnswer", "accept", nil, "")
	serve = session.Answer(t, instance, "deepProposal", "escalate", nil, "")
	proctest.RequireStep(t, serve, "deepUser")
	return world, session, x, serve
}

func TestAssisted_ASpentCheckIsPutToThePerson(t *testing.T) {
	_, _, _, serve := heldDeepUser(t, "assisted-held-serve")
	if serve.Parked != nil || serve.PendingChooser == nil || serve.PendingChooser.Kind != "user" {
		t.Fatalf("the spent check should be put to the person, not parked for a resume, got parked %+v chooser %+v", serve.Parked, serve.PendingChooser)
	}
	if !strings.HasPrefix(serve.Goal, "put the choice to the person") {
		t.Errorf("the goal should have the agent ask the person, got %q", serve.Goal)
	}
	text := test.Untagged(serve.Instructions)
	for _, want := range []string{
		"Held at deepUser for the person: blocked again after 1 unattended revise.",
		"relay their answer verbatim as `userWords`",
		"Set out each point to the user in their language",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the held step should carry %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Answer **revise** with your reason") {
		t.Errorf("the held step should not address the agent's own answer:\n%s", text)
	}
	properties, _ := serve.ReportSchema["properties"].(map[string]any)
	choice, _ := properties["choice"].(map[string]any)
	required, _ := serve.ReportSchema["required"].([]string)
	if enum, _ := choice["enum"].([]any); len(enum) != 3 || properties["reason"] != nil || !slices.Contains(required, "userWords") {
		t.Errorf("the held step's schema should take the person's words for any option, got %v", serve.ReportSchema)
	}
}

func TestAssisted_ThePersonsRelayedAnswerSettlesASpentCheck(t *testing.T) {
	t.Run("directly", func(t *testing.T) {
		_, session, x, _ := heldDeepUser(t, "assisted-held-direct")
		serve := session.Answer(t, x.instance, "deepUser", "revise", map[string]any{"body": heldBody + " Twice, and in scope."}, "confirmed")
		proctest.RequireStep(t, serve, "playback")
	})
	t.Run("after resume_session", func(t *testing.T) {
		world, session, x, _ := heldDeepUser(t, "assisted-held-resume")
		world.Restart(t)
		sessions, err := world.App.ListWorkflowSessions(t.Context(), world.Identity, "proctest")
		if err != nil {
			t.Fatal(err)
		}
		if parked := parkedMove(sessions, session.ID); parked == nil || parked.Step != "deepUser" {
			t.Fatalf("the held check should be listed as parked after a restart, got %+v", sessions)
		}
		resumed, served := world.Resume(t, session.ID, "assisted-held-resume")
		if serve := servedInstance(served.Open, x.instance); serve == nil || serve.Step != "deepUser" || serve.PendingChooser == nil {
			t.Fatalf("the resume should put the held check to the person, got %+v", serve)
		}
		serve := resumed.Answer(t, x.instance, "deepUser", "settle", nil, "confirmed, the draft stands")
		proctest.RequireStep(t, serve, "playback")
		sessions, err = world.App.ListWorkflowSessions(t.Context(), world.Identity, "proctest")
		if err != nil {
			t.Fatal(err)
		}
		if parked := parkedMove(sessions, session.ID); parked != nil && parked.Step == "deepUser" {
			t.Errorf("the person's answer should end the hold at the check, got %+v", parked)
		}
	})
}

func TestAssisted_TheAgentCannotAnswerASpentCheck(t *testing.T) {
	world, session, x, _ := heldDeepUser(t, "assisted-held-agent")
	_, err := session.DecideErr(t, x.instance, "deepUser", "revise", map[string]any{"body": heldBody}, "fold in the point")
	if err == nil || !strings.Contains(err.Error(), "deepUser is the person's now") || !strings.Contains(err.Error(), "as userWords") {
		t.Fatalf("the agent's own answer should be refused, naming how the person's answer is relayed, got %v", err)
	}
	_, err = session.WF.Abandon(t.Context(), world.Identity, x.instance, "the point stays open")
	if err == nil || !strings.Contains(err.Error(), "the person drops it by answering abort") {
		t.Fatalf("the agent should not abandon a check held for the person, got %v", err)
	}
}
