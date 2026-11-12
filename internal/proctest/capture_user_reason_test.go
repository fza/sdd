package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// A decision captured with a user present carries the user's own words for
// its reason, and a directive's for its intent: assemble holds until they
// are reported and tells the agent to ask the user for them.

const userReasonAsk = "ask the user for the decision's reason"

func decisionDraft(kind string) map[string]any {
	draft := map[string]any{
		"body":        "The parser rejects empty input with an explicit error, since empty input hid upstream bugs twice.",
		"entryKind":   kind,
		"layer":       "tactical",
		"confidence":  "high",
		"widenReport": "searched the graph for prior parser decisions before drafting",
	}
	if kind == "directive" {
		draft["intent"] = "pending"
	}
	return draft
}

func requireUserWordsHeld(t *testing.T, serve *sdd.WorkflowServe, field string) {
	t.Helper()
	proctest.RequireStep(t, serve, "assemble")
	if !hasDiagnostic(serve, userReasonAsk) || !hasDiagnostic(serve, field) {
		t.Fatalf("assemble should hold on the missing %s and tell the agent to ask the user, got diagnostics %v", field, serve.Diagnostics)
	}
}

func TestCapture_DirectiveWithoutTheUsersReasonIsHeld(t *testing.T) {
	world := proctest.NewWorld(t)
	session := world.Open(t, "reason-directive-held")
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, decisionDraft("directive"))
	requireUserWordsHeld(t, serve, "reasonWords")
}

func TestCapture_DirectiveWithoutTheUsersIntentIsHeld(t *testing.T) {
	world := proctest.NewWorld(t)
	session := world.Open(t, "reason-intent-held")
	serve := session.Start(t, "capture", nil)
	draft := decisionDraft("directive")
	draft["reasonWords"] = "empty input hid upstream bugs twice"
	serve = session.Report(t, serve.Instance, draft)
	requireUserWordsHeld(t, serve, "intentWords")
	if hasDiagnostic(serve, "reasonWords") {
		t.Errorf("reported reasonWords should no longer be owed, got %v", serve.Diagnostics)
	}
}

func TestCapture_DirectiveWithTheUsersWordsAdvances(t *testing.T) {
	world := proctest.NewWorld(t)
	session := world.Open(t, "reason-directive-advances")
	serve := session.Start(t, "capture", nil)
	draft := decisionDraft("directive")
	draft["reasonWords"] = "empty input hid upstream bugs twice"
	draft["intentWords"] = "build it now"
	serve = session.Report(t, serve.Instance, draft)
	proctest.RequireStep(t, serve, "playback")
	for _, want := range []string{"empty input hid upstream bugs twice", "build it now"} {
		if !strings.Contains(serve.Instructions, want) {
			t.Errorf("playback should show the user's quoted words %q", want)
		}
	}
}

func TestCapture_ActivityNeedsTheUsersReasonOnly(t *testing.T) {
	world := proctest.NewWorld(t)
	session := world.Open(t, "reason-activity")
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, decisionDraft("activity"))
	requireUserWordsHeld(t, serve, "reasonWords")
	serve = session.Report(t, serve.Instance, map[string]any{"reasonWords": "the release needs it"})
	if serve.Step == "assemble" {
		t.Fatalf("an activity with the user's reason should advance without intent words, held with %v", serve.Diagnostics)
	}
}

func TestCapture_SignalNeedsNoUserWords(t *testing.T) {
	_, session := newCaptureWorld(t, "reason-signal")
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
}

func TestCapture_AutonomousDecisionNeedsNoUserWords(t *testing.T) {
	world := proctest.NewWorld(t, proctest.WithAutonomousDialogue())
	session := world.Open(t, "reason-autonomous")
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, decisionDraft("directive"))
	proctest.RequireStep(t, serve, "playback")
	// An adjust passes the assemble gate again, now past the writing guide.
	serve = session.Decide(t, serve.Instance, "playback", "adjust", map[string]any{"confidence": "medium"}, "the evidence is thinner")
	proctest.RequireStep(t, serve, "playback")
}
