package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
)

// A decision captured with a user present may carry the user's own words for
// its reason, and a directive's for its intent; none are owed.

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

func TestCapture_DirectiveWithoutTheUsersWordsAdvances(t *testing.T) {
	world := proctest.NewWorld(t)
	session := world.Open(t, "reason-directive-no-words")
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, decisionDraft("directive"))
	proctest.RequireStep(t, serve, "playback")
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

func TestCapture_SignalNeedsNoUserWords(t *testing.T) {
	_, session := newCaptureWorld(t, "reason-signal")
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
}

func TestCapture_AutonomousDecisionNeedsNoUserWords(t *testing.T) {
	world := proctest.NewWorld(t, proctest.WithAutonomousDialogue(), clientActor("reason-autonomous"))
	session := world.Open(t, "reason-autonomous")
	serve := session.Start(t, "capture", nil)
	draft := decisionDraft("directive")
	draft["reason"] = "Empty input hid upstream bugs twice."
	serve = session.Report(t, serve.Instance, draft)
	proctest.RequireStep(t, serve, "playback")
	// An adjust passes the assemble gate again, now past the writing guide.
	serve = session.Decide(t, serve.Instance, "playback", "adjust", map[string]any{"confidence": "medium"}, "the evidence is thinner")
	proctest.RequireStep(t, serve, "playback")
}

func TestCapture_ReasonStatesWhyNeverWhat(t *testing.T) {
	world := proctest.NewWorld(t)
	session := world.Open(t, "reason-why-not-what")
	serve := session.Start(t, "capture", nil)
	want := "The reason states why, never what: words stating the decision itself never fill `reasonWords`."
	if !strings.Contains(serve.Instructions, want) {
		t.Fatalf("assemble should tell the agent the reason states why:\n%s", serve.Instructions)
	}
}

// A directive's posture is an SDD mechanic the user does not know: the agent
// speaks of it in the user's terms, wherever it shows it.
func TestCapture_IntentIsShownInTheUsersTerms(t *testing.T) {
	const noPosture = "never name the posture"
	world := proctest.NewWorld(t)
	session := world.Open(t, "intent-users-terms")
	serve := session.Start(t, "capture", nil)
	if !strings.Contains(serve.Instructions, noPosture) {
		t.Errorf("assemble should tell the agent to speak of the intent without naming the posture:\n%s", serve.Instructions)
	}
	draft := decisionDraft("directive")
	draft["reasonWords"] = "empty input hid upstream bugs twice"
	serve = session.Report(t, serve.Instance, draft)
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
