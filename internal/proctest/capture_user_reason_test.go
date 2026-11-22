package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// No decision owes the user's words for its reason, and no directive for its
// intent: words the user gives are carried quoted, and a directive still
// needs its intent value.

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

func requireNoUserWordsOwed(t *testing.T, serve *sdd.WorkflowServe) {
	t.Helper()
	proctest.RequireStep(t, serve, "playback")
	for _, field := range []string{"reasonWords", "intentWords"} {
		if hasDiagnostic(serve, field) {
			t.Errorf("no draft should owe %s, got diagnostics %v", field, serve.Diagnostics)
		}
	}
}

func TestCapture_DirectiveWithoutTheUsersWordsAdvances(t *testing.T) {
	world := proctest.NewWorld(t)
	session := world.Open(t, "reason-directive-advances-bare")
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, decisionDraft("directive"))
	requireNoUserWordsOwed(t, serve)
}

func TestCapture_DirectiveWithoutItsIntentValueIsHeld(t *testing.T) {
	world := proctest.NewWorld(t)
	session := world.Open(t, "reason-intent-value-held")
	serve := session.Start(t, "capture", nil)
	draft := decisionDraft("directive")
	delete(draft, "intent")
	serve = session.Report(t, serve.Instance, draft)
	proctest.RequireStep(t, serve, "assemble")
	if !hasDiagnostic(serve, "intent") {
		t.Fatalf("a directive without its intent value should hold assemble naming intent, got %v", serve.Diagnostics)
	}
	serve = session.Report(t, serve.Instance, map[string]any{"intent": "guiding"})
	requireNoUserWordsOwed(t, serve)
}

func TestCapture_DirectiveWithTheUsersWordsShowsThem(t *testing.T) {
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

func TestCapture_OtherDecisionsOweNoReason(t *testing.T) {
	for _, kind := range []string{"plan", "activity"} {
		t.Run(kind, func(t *testing.T) {
			world := proctest.NewWorld(t)
			session := world.Open(t, "reason-"+kind)
			serve := session.Start(t, "capture", nil)
			draft := decisionDraft(kind)
			if kind == "plan" {
				draft["body"] = draft["body"].(string) + "\n\n## Acceptance criteria\n\n- [ ] The parser refuses empty input."
			}
			serve = session.Report(t, serve.Instance, draft)
			requireNoUserWordsOwed(t, serve)
		})
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

func TestCapture_ReasonStatesWhyNeverWhat(t *testing.T) {
	world := proctest.NewWorld(t)
	session := world.Open(t, "reason-why-not-what")
	serve := session.Start(t, "capture", nil)
	want := "No decision owes a reason, and no directive owes the user's words for its intent. Words the user gives for a decision's reason go to `reasonWords`, and for a directive's intent to `intentWords`, quoted as the user said them. The reason states why, never what: words stating the decision itself never fill `reasonWords`."
	if !strings.Contains(serve.Instructions, want) {
		t.Fatalf("assemble should tell the agent no words are owed and the reason states why:\n%s", serve.Instructions)
	}
}

// A directive's posture is an SDD mechanic the user does not know: the agent
// speaks of it in the user's terms, at assemble and at playback.
func TestCapture_IntentIsNamedInTheUsersTerms(t *testing.T) {
	const noPosture = "never name the posture"
	world := proctest.NewWorld(t)
	session := world.Open(t, "intent-users-terms")
	serve := session.Start(t, "capture", nil)
	if !strings.Contains(serve.Instructions, noPosture) {
		t.Errorf("assemble should tell the agent to speak of the intent without naming the posture:\n%s", serve.Instructions)
	}
	serve = session.Report(t, serve.Instance, decisionDraft("directive"))
	proctest.RequireStep(t, serve, "playback")
	if !strings.Contains(serve.Instructions, noPosture) {
		t.Errorf("playback should tell the agent to show the intent without naming the posture:\n%s", serve.Instructions)
	}
}

// The quoted words stay the user's: an answer that changes them quotes them
// from what the user says in that answer, and a wording change to the draft
// goes into the body instead.
func TestCapture_UserWordsChangeOnlyToWhatTheUserSays(t *testing.T) {
	world := proctest.NewWorld(t)
	session := world.Open(t, "reason-words-quoted")
	serve := session.Start(t, "capture", nil)
	draft := decisionDraft("directive")
	draft["reasonWords"] = "empty input hid upstream bugs twice"
	draft["intentWords"] = "the parser should reject empty input with a clear error"
	serve = session.Report(t, serve.Instance, draft)
	proctest.RequireStep(t, serve, "playback")

	wording := `Looks right, but say "an explicit error" instead of "a clear error".`
	_, err := session.AnswerErr(t, serve.Instance, "playback", "adjust",
		map[string]any{"intentWords": "the parser should reject empty input with an explicit error"}, wording)
	if err == nil || !strings.Contains(err.Error(), "intentWords") || !strings.Contains(err.Error(), "quote") {
		t.Fatalf("intent words the user did not say in the answer should be refused, got %v", err)
	}

	pending := "The parser still returns an empty result today. Make it pending, the change still has to be made."
	serve = session.Answer(t, serve.Instance, "playback", "adjust",
		map[string]any{"intent": "pending", "intentWords": "the change still has to be made"}, pending)
	proctest.RequireStep(t, serve, "playback")
}
