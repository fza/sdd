package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
)

// The autonomous dialogue mode (20261029-150854-d-tac-b2f): the agent takes
// every choice a base procedure hands over and keeps the pre-flight override
// for the user.

func TestAutonomousDialogue_ShellServesTheAgentAndTheAgentConcludes(t *testing.T) {
	world := proctest.NewWorld(t, proctest.WithAutonomousDialogue())
	session, serve := openShell(t, world, "autonomous-shell")
	for _, want := range []string{"working on your own", "end the session yourself", "Start the move the work calls for yourself"} {
		if !strings.Contains(serve.Instructions, want) {
			t.Errorf("an autonomous shell should tell the agent it decides; missing %q", want)
		}
	}
	for _, unwanted := range []string{"offer to wrap the session up", "Recommend, never auto-run", "Be a colleague"} {
		if strings.Contains(serve.Instructions, unwanted) {
			t.Errorf("an autonomous shell should leave out wording for a user; found %q", unwanted)
		}
	}
	done := session.Decide(t, serve.Instance, "junction", "conclude", nil, "the work is done")
	proctest.RequireStatus(t, done, "completed")
}

func TestAutonomousDialogue_GuidedShellKeepsItsWording(t *testing.T) {
	world := proctest.NewWorld(t)
	session, serve := openShell(t, world, "guided-shell")
	if strings.Contains(serve.Instructions, "working on your own") || !strings.Contains(serve.Instructions, "offer to wrap the session up") {
		t.Error("a guided shell should serve its wording for a user")
	}
	if _, err := session.DecideErr(t, serve.Instance, "junction", "conclude", nil, "the work is done"); err == nil {
		t.Error("a guided session should refuse an agent's own answer")
	}
}

func TestAutonomousDialogue_AgentConfirmsACapture(t *testing.T) {
	_, session := newCaptureWorld(t, "autonomous-capture", proctest.WithAutonomousDialogue())
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	if !strings.Contains(serve.Instructions, "Verify the draft yourself") || strings.Contains(serve.Instructions, "Present the draft to the user") {
		t.Errorf("an autonomous playback should ask the agent to verify the draft itself, got %q", serve.Instructions)
	}
	serve = session.Decide(t, serve.Instance, "playback", "confirm", nil, "the draft states the fixture gap")
	proctest.RequireStep(t, serve, "verifySummary")
}

func TestAutonomousDialogue_OverrideStaysTheUsers(t *testing.T) {
	world, session := newCaptureWorld(t, "autonomous-override", proctest.WithAutonomousDialogue())
	world.LLM.PreflightFindings = []proctest.PreflightFinding{{Severity: "high", Category: "test-block", Observation: "Review this finding."}}
	serve := session.Start(t, "capture", nil)
	session.Report(t, serve.Instance, captureDraft())
	serve = session.Decide(t, serve.Instance, "playback", "confirm", nil, "the draft holds")
	proctest.RequireStep(t, serve, "reviseOrOverride")
	if _, err := session.DecideErr(t, serve.Instance, "reviseOrOverride", "override", nil, "the finding does not apply"); err == nil || !strings.Contains(err.Error(), "stays the user's choice") {
		t.Fatalf("the agent should not override a failed pre-flight on its own, got %v", err)
	}
	world.LLM.PreflightFindings = nil
	serve = session.Answer(t, serve.Instance, "reviseOrOverride", "override", nil, "override it, the finding does not apply")
	proctest.RequireStep(t, serve, "verifySummary")
}
