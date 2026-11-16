package proctest_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// In an autonomous session the checks a capture runs are answered by the
// agent within a bound: one revision or retry, never a bypass. A second block
// parks the capture for a person.

// blockedAutonomousCapture drives an autonomous capture to a pre-flight that
// keeps a high finding open.
func blockedAutonomousCapture(t *testing.T, connID string) (*proctest.World, *proctest.Session, string) {
	t.Helper()
	world, session := newCaptureWorld(t, connID, proctest.WithAutonomousDialogue())
	world.LLM.PreflightFindings = []proctest.PreflightFinding{{Severity: "high", Category: "test-block", Observation: "Review this finding."}}
	serve := session.Start(t, "capture", nil)
	session.Report(t, serve.Instance, captureDraft())
	serve = session.Decide(t, serve.Instance, "playback", "confirm", nil, "the draft holds")
	proctest.RequireStep(t, serve, "reviseOrOverride")
	return world, session, serve.Instance
}

// parkAfterSecondBlock revises once, confirms, and returns the serve the
// second block parks at.
func parkAfterSecondBlock(t *testing.T, session *proctest.Session, instance string) *sdd.WorkflowServe {
	t.Helper()
	serve := session.Decide(t, instance, "reviseOrOverride", "revise", map[string]any{"body": "A tactical gap: the fixture observes something twice."}, "fold in the finding")
	proctest.RequireStep(t, serve, "playback")
	return session.Decide(t, instance, "playback", "confirm", nil, "the revision holds")
}

func TestUnattended_AutonomousSessionRefusesABypass(t *testing.T) {
	cases := []struct {
		name, choice, userWords, reason, want string
	}{
		{name: "agent override", choice: "override", reason: "the finding does not apply", want: `reviseOrOverride refuses "override" in an autonomous session (unattended rule`},
		{name: "user words override", choice: "override", userWords: "override it", want: "reviseOrOverride refuses user words in an autonomous session (unattended rule"},
		{name: "user words revise", choice: "revise", userWords: "revise it", want: "reviseOrOverride refuses user words in an autonomous session (unattended rule"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, session, instance := blockedAutonomousCapture(t, "unattended-refuse")
			report := map[string]any{"chooser": "reviseOrOverride", "choice": tc.choice}
			if tc.userWords != "" {
				report["userWords"] = tc.userWords
			}
			if tc.reason != "" {
				report["reason"] = tc.reason
			}
			_, err := session.WF.Advance(t.Context(), session.World.Identity, sdd.WorkflowAdvanceRequest{Instance: instance, Report: report})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the bypass should be refused naming the step and the rule, got %v", err)
			}
		})
	}
}

func TestUnattended_TheStepServesOnlyItsAnswer(t *testing.T) {
	_, session, instance := blockedAutonomousCapture(t, "unattended-schema")
	all, err := session.WF.ServeAll(t.Context(), session.World.Identity)
	if err != nil {
		t.Fatal(err)
	}
	serve := servedInstance(all.Open, instance)
	choice, _ := serve.ReportSchema["properties"].(map[string]any)["choice"].(map[string]any)
	if enum, _ := choice["enum"].([]any); len(enum) != 1 || enum[0] != "revise" {
		t.Errorf("an unattended step should offer the agent its one answer, got %v", choice["enum"])
	}
	if !strings.Contains(serve.Instructions, "only a person overrides") || strings.Contains(serve.Instructions, "relay their words") {
		t.Errorf("an unattended step should address the agent, got:\n%s", serve.Instructions)
	}
}

func TestUnattended_ASecondBlockParksTheCapture(t *testing.T) {
	_, session, instance := blockedAutonomousCapture(t, "unattended-park")
	serve := parkAfterSecondBlock(t, session, instance)
	proctest.RequireStep(t, serve, "reviseOrOverride")
	proctest.RequireStatus(t, serve, "running")
	resume := "resume_session " + string(session.ID)
	if serve.Parked == nil || serve.Parked.Step != "reviseOrOverride" || serve.Parked.Reason == "" || serve.Parked.Resume != resume {
		t.Fatalf("the second block should park the capture at its step with a reason and the resume call, got %+v", serve.Parked)
	}
	if want := "End your turn with: parked at reviseOrOverride: " + resume; !strings.Contains(serve.Instructions, want) {
		t.Errorf("the parked serve should tell the agent how to end its turn, got %q", serve.Instructions)
	}
	if serve.PendingChooser != nil || serve.ReportSchema != nil {
		t.Errorf("a parked serve should offer the agent no answer, got %+v and %v", serve.PendingChooser, serve.ReportSchema)
	}
	if _, err := session.DecideErr(t, instance, "reviseOrOverride", "revise", map[string]any{"body": "Another try."}, "once more"); err == nil || !strings.Contains(err.Error(), "reviseOrOverride is parked") || !strings.Contains(err.Error(), "dialogue.mode guided") {
		t.Fatalf("a parked step should refuse the agent's further answers and name the guided mode a person answers in, got %v", err)
	}
}

func TestUnattended_ATimedOutPreflightRetriesOnceThenParks(t *testing.T) {
	world, session := newCaptureWorld(t, "unattended-timeout", proctest.WithAutonomousDialogue())
	world.LLM.PreflightErr = preflightTimedOut()
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Report(t, instance, captureDraft())
	serve = session.Decide(t, instance, "playback", "confirm", nil, "the draft holds")
	proctest.RequireStep(t, serve, "preflightTimedOut")
	if _, err := session.DecideErr(t, instance, "preflightTimedOut", "skip", nil, "skip it"); err == nil || !strings.Contains(err.Error(), `preflightTimedOut refuses "skip"`) {
		t.Fatalf("a skip should be refused in an autonomous session, got %v", err)
	}
	serve = session.Decide(t, instance, "preflightTimedOut", "retry", nil, "a slow call may pass")
	if serve.Parked == nil || serve.Parked.Step != "preflightTimedOut" {
		t.Fatalf("a second timeout should park the capture, got step %s parked %+v", serve.Step, serve.Parked)
	}
}

func TestUnattended_AParkedCaptureResumesForAPerson(t *testing.T) {
	world, session, instance := blockedAutonomousCapture(t, "unattended-resume")
	parkAfterSecondBlock(t, session, instance)

	world.Restart(t)
	sessions, err := world.App.ListWorkflowSessions(t.Context(), world.Identity, "proctest")
	if err != nil {
		t.Fatal(err)
	}
	if parked := parkedMove(sessions, session.ID); parked == nil || parked.Step != "reviseOrOverride" {
		t.Fatalf("the parked capture should be listed after a restart, got %+v", sessions)
	}

	world.SetDialogue("guided")
	resumed, serves, err := world.App.ResumeWorkflow(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: session.ID, ClientName: "person"})
	if err != nil {
		t.Fatal(err)
	}
	session.WF = resumed
	serve := servedInstance(serves.Open, instance)
	if serve == nil || serve.Step != "reviseOrOverride" || serve.Parked != nil || serve.PendingChooser == nil {
		t.Fatalf("a guided resume should put the parked step to the person, got %+v", serve)
	}
	world.LLM.PreflightFindings = nil
	serve = session.Answer(t, instance, "reviseOrOverride", "override", nil, "override it, the finding does not apply")
	proctest.RequireStep(t, serve, "verifySummary")
	sessions, err = world.App.ListWorkflowSessions(t.Context(), world.Identity, "proctest")
	if err != nil {
		t.Fatal(err)
	}
	if parked := parkedMove(sessions, session.ID); parked != nil {
		t.Errorf("a person's answer should end the parking, got %+v", parked)
	}
}

func parkedMove(sessions []sdd.WorkflowSessionSummary, id sdd.SessionID) *sdd.WorkflowParked {
	for _, session := range sessions {
		if session.Session != id {
			continue
		}
		for _, open := range session.Open {
			if open.Parked != nil {
				return open.Parked
			}
		}
	}
	return nil
}

func servedInstance(serves []sdd.WorkflowServe, instance string) *sdd.WorkflowServe {
	for i := range serves {
		if serves[i].Instance == instance {
			return &serves[i]
		}
	}
	return nil
}

// Dropping a blocked capture is the user's, as overriding its check is: an
// agent in auto mode cannot abandon it.
func TestUnattended_TheAgentCannotAbandonABlockedCapture(t *testing.T) {
	world, session, instance := blockedAutonomousCapture(t, "unattended-abandon")
	_, err := session.WF.Abandon(t.Context(), world.Identity, instance, "the finding holds")
	if err == nil || !strings.Contains(err.Error(), "is not abandoned in an autonomous session") {
		t.Fatalf("an agent should not abandon a capture blocked at an unattended step, got %v", err)
	}
}

// Ending the session by its handle is the person's door to drop a blocked
// capture: sdd sessions abandon ends it, while the agent's abandon by handle
// (marked Agent, as the MCP surface sends it) is refused like its per-move one.
func TestUnattended_OnlyThePersonEndsTheSessionOfABlockedCapture(t *testing.T) {
	world, session, instance := blockedAutonomousCapture(t, "unattended-abandon-session")
	_, err := world.App.AbandonWorkflowSession(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: session.ID, Agent: true}, "the finding holds")
	if err == nil || !strings.Contains(err.Error(), "is not abandoned in an autonomous session") {
		t.Fatalf("the agent's abandon by session handle should be refused, got %v", err)
	}
	result, err := world.App.AbandonWorkflowSession(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: session.ID}, "dropped by its owner")
	if err != nil {
		t.Fatalf("sdd sessions abandon should end the session with the blocked capture, got %v", err)
	}
	if !result.Abandoned || !slices.ContainsFunc(result.Discarded, func(d sdd.WorkflowInstanceSummary) bool { return d.Instance == instance }) {
		t.Fatalf("the session should end with the blocked capture discarded, got %+v", result)
	}
}
