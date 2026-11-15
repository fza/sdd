package engine_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

const repeatedWordsMachine = `steps:
    - id: show
      chooser: user
      options:
          - {choice: adjust, to: show}
          - {choice: confirm, to: end(completed)}`

const repeatedWordsBody = "## unit: show\n\nShow the draft and ask the user.\n"

func repeatedWordsSession(t *testing.T, sink engine.EventSink) (*engine.Session, *engine.Spec, string) {
	t.Helper()
	spec := mustParseLanes(t, repeatedWordsMachine, repeatedWordsBody)
	session := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).NewSession("s_words", "tester", sink)
	sv, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := session.Answer(sv.Instance, "show", "adjust", nil, `say "an explicit error"`); err != nil {
		t.Fatalf("adjust: %v", err)
	}
	return session, spec, sv.Instance
}

// The user's words asking for a change say nothing about the changed draft
// the step serves next: relaying them again as another choice answers for
// the user before they saw it.
func TestUserWordsOfAnEarlierChoiceAnswerNoOtherChoice(t *testing.T) {
	sink := &recordingSink{}
	session, spec, instance := repeatedWordsSession(t, sink)

	_, err := session.Answer(instance, "show", "confirm", nil, `say "an explicit error"`)
	if err == nil || !strings.Contains(err.Error(), "repeat the user's words") {
		t.Fatalf("a confirm carrying the adjust's words should be refused, got %v", err)
	}

	replayed := replayAgainst(t, spec, sink.events)
	_, err = replayed.Answer(instance, "show", "confirm", nil, `say "an explicit error"`)
	if err == nil || !strings.Contains(err.Error(), "repeat the user's words") {
		t.Fatalf("the refusal should survive a replay, got %v", err)
	}

	if _, err := session.Answer(instance, "show", "confirm", nil, "Yes, write it."); err != nil {
		t.Fatalf("the user's own answer to the changed draft should confirm: %v", err)
	}
}

// The same words for the same choice are a fresh answer, as a user saying
// "Yes, write it." at each playback.
func TestUserWordsMayRepeatForTheSameChoice(t *testing.T) {
	session, _, instance := repeatedWordsSession(t, &recordingSink{})

	if _, err := session.Answer(instance, "show", "adjust", nil, `say "an explicit error"`); err != nil {
		t.Fatalf("the same words for the same choice should stand: %v", err)
	}
}
