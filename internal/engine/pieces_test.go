package engine_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
	test "github.com/networkteam/sdd/internal/testsupport"
)

const (
	stepText  = "Draft one entry with the user, dialogue first. Check its grounding against the graph before you write. Keep the summary under 280 characters so the listing stays short."
	introText = "Open the session with a short briefing. Name the open threads the dialogue carries, then ask the user where to begin today."
)

var (
	stepLane  = engine.Piece{Name: "lane step", Hash: "h1"}
	introLane = engine.Piece{Name: "lane intro", Hash: "h2"}
	entry     = engine.Piece{Name: "20260101-000000-s-tac-aaa", Hash: "e1"}
)

func newPieceSession(sink engine.EventSink) *engine.Session {
	return engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).NewSession("s_pieces", "tester", sink)
}

func serve(session *engine.Session, p engine.Piece, text string) bool {
	return session.ServePiece(p, nil, text)
}

// respond serves the pieces as one response and returns the question it ends
// with.
func respond(t *testing.T, session *engine.Session, answer string, pieces map[engine.Piece]string) (held map[engine.Piece]bool, question string) {
	t.Helper()
	session.BeginCall(answer, nil)
	held = map[engine.Piece]bool{}
	for p, text := range pieces {
		held[p] = serve(session, p, text)
	}
	session.RecordServed()
	return held, session.Ask()
}

// answer answers question from texts as an agent holding them would.
func answer(t *testing.T, question string, texts ...string) string {
	t.Helper()
	_, ask, ok := test.LastQuestion(question)
	if !ok {
		t.Fatalf("a response should end with a question, got %q", question)
	}
	words, ok := test.AnswerQuestion(ask, texts)
	if !ok {
		t.Fatalf("the question should be answerable from the texts served, got %q", question)
	}
	return words
}

func TestARightAnswerServesRepeatsShort(t *testing.T) {
	session := newPieceSession(nil)
	pieces := map[engine.Piece]string{stepLane: stepText, introLane: introText}
	held, question := respond(t, session, "", pieces)
	if held[stepLane] || held[introLane] {
		t.Fatal("a first serve should send every piece in full")
	}

	tests := []struct {
		name  string
		given string
		short bool
	}{
		{"lost", model.LostAnswer, false},
		{"wrong", "nothing here", false},
		{"none", "", false},
		{"right", answer(t, question, stepText, introText), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			session.BeginCall(tc.given, nil)
			for p, text := range pieces {
				if got := serve(session, p, text); got != tc.short {
					t.Errorf("%s should serve short only after a right answer", p.Name)
				}
			}
		})
	}
}

func TestTheFirstQuestionAsksAboutTheResponsesOwnText(t *testing.T) {
	session := newPieceSession(nil)
	_, question := respond(t, session, "", map[engine.Piece]string{stepLane: stepText})
	if name, _, ok := test.LastQuestion(question); !ok || name != stepLane.Name {
		t.Errorf("the first response should ask about its own text, got %q", question)
	}
}

func TestTheQuestionAsksAboutTheOldestTextByItsLatestCopy(t *testing.T) {
	session := newPieceSession(nil)
	_, q := respond(t, session, "", map[engine.Piece]string{stepLane: stepText})
	_, q = respond(t, session, answer(t, q, stepText), map[engine.Piece]string{introLane: introText})
	if name, _, _ := test.LastQuestion(q); name != stepLane.Name {
		t.Errorf("the question should ask about the text served longest ago, got %q", q)
	}
	// lost serves the step text again, which makes intro the oldest copy.
	_, q = respond(t, session, model.LostAnswer, map[engine.Piece]string{stepLane: stepText})
	if name, _, _ := test.LastQuestion(q); name != introLane.Name {
		t.Errorf("a text served in full again should no longer be the oldest, got %q", q)
	}
}

func TestNoQuestionIsAskedTwice(t *testing.T) {
	session := newPieceSession(nil)
	asked := map[string]bool{}
	_, q := respond(t, session, "", map[engine.Piece]string{stepLane: stepText})
	for range 60 {
		if asked[q] {
			t.Fatalf("a question should never be asked twice, got %q again", q)
		}
		asked[q] = true
		_, q = respond(t, session, answer(t, q, stepText), map[engine.Piece]string{stepLane: stepText})
		if q == "" {
			break
		}
	}
}

func TestNoAnswerAnswersTwoQuestions(t *testing.T) {
	session := newPieceSession(nil)
	var given []string
	_, q := respond(t, session, "", map[engine.Piece]string{stepLane: stepText})
	for q != "" {
		words := answer(t, q, stepText)
		for _, earlier := range given {
			if (model.TextQuestion{Answer: earlier}).Answered(words) {
				t.Fatalf("an answer given before should answer no later question, got %q again for %q", words, q)
			}
		}
		given = append(given, words)
		_, q = respond(t, session, words, map[engine.Piece]string{stepLane: stepText})
	}
}

func TestAnAnswerCountsAgainstEveryOpenQuestion(t *testing.T) {
	session := newPieceSession(nil)
	_, first := respond(t, session, "", map[engine.Piece]string{stepLane: stepText})
	// A second agent on the handle answers lost and gets everything in full.
	held, _ := respond(t, session, model.LostAnswer, map[engine.Piece]string{stepLane: stepText})
	if held[stepLane] {
		t.Fatal("a lost answer should serve the text in full")
	}
	// The first agent answers the question it was asked.
	right := answer(t, first, stepText)
	held, _ = respond(t, session, right, map[engine.Piece]string{stepLane: stepText})
	if !held[stepLane] {
		t.Error("a right answer to an earlier open question should still serve short")
	}
	session.BeginCall(right, nil)
	if session.Answered() {
		t.Error("a question answered once should not count an answer again")
	}
}

func TestAFailedCallKeepsItsQuestionOpen(t *testing.T) {
	session := newPieceSession(nil)
	_, q := respond(t, session, "", map[engine.Piece]string{stepLane: stepText})
	right := answer(t, q, stepText)
	session.BeginCall(right, nil)
	session.DropAnswer()
	session.RecordServed()
	session.BeginCall(right, nil)
	if !session.Answered() {
		t.Error("the question of a call that failed should stay open for the next call")
	}
}

func TestEntriesServeInFullInEveryCall(t *testing.T) {
	session := newPieceSession(nil)
	session.BeginCall("", nil)
	if session.ServeEntry(entry) {
		t.Fatal("an entry should serve in full the first time")
	}
	if !session.ServeEntry(entry) {
		t.Error("an entry served earlier in the same call should serve as its pointer")
	}
	serve(session, stepLane, stepText)
	session.RecordServed()
	q := session.Ask()

	session.BeginCall(answer(t, q, stepText), nil)
	if session.ServeEntry(entry) {
		t.Error("a right answer should not hold an entry served in an earlier call")
	}
}

func TestACarriedEntryIsHeldForTheRestOfTheCall(t *testing.T) {
	session := newPieceSession(nil)
	session.BeginCall("", nil)
	session.ServePiece(stepLane, []engine.Piece{entry}, stepText)
	if !session.HoldsEntry(entry) {
		t.Error("an entry a piece carried whole should be held for the rest of the call")
	}
}

func TestRereadAndReorientServeInFull(t *testing.T) {
	session := newPieceSession(nil)
	_, q := respond(t, session, "", map[engine.Piece]string{stepLane: stepText, introLane: introText})
	right := answer(t, q, stepText, introText)

	session.BeginCall(right, []string{"Lane Step"})
	if serve(session, stepLane, stepText) {
		t.Error("a piece listed in reread should serve in full")
	}
	if !serve(session, introLane, introText) {
		t.Error("a piece not listed in reread should serve short after a right answer")
	}

	session.BeginCall(right, nil)
	session.Reorient()
	if serve(session, introLane, introText) {
		t.Error("a reorienting call should serve every piece in full")
	}
}

func TestAChangedTextServesInFull(t *testing.T) {
	session := newPieceSession(nil)
	_, q := respond(t, session, "", map[engine.Piece]string{stepLane: stepText})
	session.BeginCall(answer(t, q, stepText), nil)
	changed := engine.Piece{Name: stepLane.Name, Hash: "h-changed"}
	if serve(session, changed, stepText+" Then stop.") {
		t.Error("a piece whose content changed should serve in full")
	}
}

func TestTextsAndQuestionsSurviveARestart(t *testing.T) {
	var log bytes.Buffer
	session := newPieceSession(engine.NewWriterSink(&log))
	_, q := respond(t, session, "", map[engine.Piece]string{stepLane: stepText})
	log.WriteString(`{"v":1,"seq":98,"event":"served_blocks","data":{"hashes":["written-before-the-text-question"]}}` + "\n")

	events, err := engine.ReadEvents(&log)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).
		ReplaySession("s_pieces", "tester", events, func(string) (*engine.Spec, error) { return nil, nil }, nil)
	if err != nil {
		t.Fatalf("replaying a log with texts, a question and an older served-memory event: %v", err)
	}
	replayed.BeginCall(answer(t, q, stepText), nil)
	if !serve(replayed, stepLane, stepText) {
		t.Error("a question asked before the restart should still be checked, and its text held")
	}
	replayed.RecordServed()
	if next := replayed.Ask(); next == q || next == "" {
		t.Errorf("a restarted session should ask a new question, got %q", next)
	}
}

func TestNoQuestionPhraseOccursInATextServedOncePerChange(t *testing.T) {
	var log bytes.Buffer
	session := newPieceSession(engine.NewWriterSink(&log))
	session.BeginCall("", nil)
	session.ServeChanged(engine.Piece{Name: "framing principles", Hash: "f1"}, stepText)
	serve(session, stepLane, stepText)
	session.RecordServed()
	if q := session.Ask(); q != "" {
		t.Errorf("a question's phrase should occur in no other text served, got %q", q)
	}

	events, err := engine.ReadEvents(&log)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).
		ReplaySession("s_pieces", "tester", events, func(string) (*engine.Spec, error) { return nil, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	replayed.BeginCall("", nil)
	if q := replayed.Ask(); q != "" {
		t.Errorf("after a restart, a question's phrase should still occur in no other text served, got %q", q)
	}
}

func TestDropPiecesSinceWithdrawsWhatACutKeptFromTheAgent(t *testing.T) {
	session := newPieceSession(nil)
	session.BeginCall("", nil)
	serve(session, stepLane, stepText)
	mark := session.PieceMark()
	serve(session, introLane, introText)
	session.DropPiecesSince(mark)

	if session.Holds(introLane) {
		t.Error("a piece withdrawn after a cut should not count as served earlier in the call")
	}
	if !session.Holds(stepLane) {
		t.Error("a piece served before the mark should stay served")
	}
	session.RecordServed()
	if q := session.Ask(); strings.Contains(q, introLane.Name) {
		t.Errorf("a withdrawn text should be no question's text, got %q", q)
	}
}
