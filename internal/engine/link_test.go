package engine_test

import (
	"bytes"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/test"
)

const reportText = "Report the outcome in one line. Name every entry the move wrote and the open threads it leaves for the next dialogue."

var reportLane = engine.Piece{Name: "lane report", Hash: "h3"}

// loggedSession is a session whose log the test reads back, as a linking
// session reads the log of the one it links.
type loggedSession struct {
	*engine.Session
	log *bytes.Buffer
}

func newLoggedSession(t *testing.T, id string) loggedSession {
	t.Helper()
	var log bytes.Buffer
	return loggedSession{
		Session: engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).NewSession(id, "tester", engine.NewWriterSink(&log)),
		log:     &log,
	}
}

func (s loggedSession) events(t *testing.T) []engine.Event {
	t.Helper()
	events, err := engine.ReadEvents(bytes.NewReader(s.log.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	return events
}

// linkTo opens a session linking earlier, carrying what it holds.
func linkTo(t *testing.T, id string, earlier loggedSession) loggedSession {
	t.Helper()
	carried, recorded := engine.HeldTexts(earlier.events(t))
	if !recorded {
		t.Fatal("a session that served texts should hold a record of them")
	}
	linked := newLoggedSession(t, id)
	linked.Link(earlier.ID, carried, engine.AuthoredTexts(earlier.events(t)))
	return linked
}

// questionName is the text a response's first question asks about.
func questionName(t *testing.T, line string) string {
	t.Helper()
	questions := test.Questions(line)
	if len(questions) == 0 {
		t.Fatalf("a response should end with a question, got %q", line)
	}
	return questions[0].Name
}

func TestLostEmptiesTheHeldSet(t *testing.T) {
	for _, given := range []string{model.LostAnswer, "nothing here"} {
		t.Run(given, func(t *testing.T) {
			session := newPieceSession(nil)
			_, q := respond(t, session, "", map[engine.Piece]string{introLane: introText})
			_, _ = respond(t, session, answer(t, q, introText), map[engine.Piece]string{stepLane: stepText})

			held, q := respond(t, session, given, map[engine.Piece]string{stepLane: stepText})
			if held[stepLane] {
				t.Fatal("the response after lost or a wrong answer should come in full")
			}
			if name := questionName(t, q); name != stepLane.Name {
				t.Fatalf("only texts served since the held set emptied should be asked about, got %q", q)
			}
			session.BeginCall(answer(t, q, stepText), nil)
			if !serve(session, stepLane, stepText) {
				t.Error("a text served after the held set emptied should serve short on a right answer")
			}
			if serve(session, introLane, introText) {
				t.Error("a text served before the held set emptied should no longer count as held")
			}
		})
	}
}

// A right answer to a question asked before another caller's lost holds
// only texts served since the held set emptied.
func TestAnAnswerFromBeforeLostHoldsOnlyLaterTexts(t *testing.T) {
	session := newPieceSession(nil)
	_, q := respond(t, session, "", map[engine.Piece]string{introLane: introText})
	_, _ = respond(t, session, model.LostAnswer, map[engine.Piece]string{stepLane: stepText})
	session.BeginCall(answer(t, q, introText), nil)
	if !serve(session, stepLane, stepText) {
		t.Error("a text served after the held set emptied should serve short on the earlier question's right answer")
	}
	if serve(session, introLane, introText) {
		t.Error("a text served before the held set emptied should serve in full whatever question the answer answers")
	}
}

func TestAFailedLostCallKeepsTheHeldSet(t *testing.T) {
	session := newPieceSession(nil)
	_, q := respond(t, session, "", map[engine.Piece]string{stepLane: stepText})
	session.BeginCall(model.LostAnswer, nil)
	session.DropAnswer()
	session.RecordServed()
	session.BeginCall(answer(t, q, stepText), nil)
	if !serve(session, stepLane, stepText) {
		t.Error("a call that failed should leave the held set as it was")
	}
}

func TestOneFullResponseAfterLostServesShortAgain(t *testing.T) {
	session := newPieceSession(nil)
	all := map[engine.Piece]string{stepLane: stepText, introLane: introText, reportLane: reportText}
	_, q := respond(t, session, "", map[engine.Piece]string{introLane: introText})
	_, q = respond(t, session, answer(t, q, introText), map[engine.Piece]string{stepLane: stepText})
	_, _ = respond(t, session, answer(t, q, introText, stepText), map[engine.Piece]string{reportLane: reportText})

	// A compaction removed every text: one lost, one full response.
	held, q := respond(t, session, model.LostAnswer, all)
	for p, short := range held {
		if short {
			t.Errorf("%s should come in full after lost", p.Name)
		}
	}
	held, _ = respond(t, session, answer(t, q, stepText, introText, reportText), all)
	for p, short := range held {
		if !short {
			t.Errorf("%s should serve short on the right answer after the full response", p.Name)
		}
	}
}

func TestALinkedSessionCarriesTheTextsItsEarlierSessionHolds(t *testing.T) {
	first := newLoggedSession(t, "s_first")
	_, q := respond(t, first.Session, "", map[engine.Piece]string{stepLane: stepText})
	_, _ = respond(t, first.Session, answer(t, q, stepText), map[engine.Piece]string{introLane: introText})

	second := linkTo(t, "s_second", first)
	held, q := respond(t, second.Session, "", map[engine.Piece]string{reportLane: reportText})
	if held[reportLane] {
		t.Fatal("the opening response of a linked session should come in full")
	}
	if name := questionName(t, q); name != stepLane.Name {
		t.Fatalf("the opening should ask about the oldest carried text by its latest copy, got %q", q)
	}

	right := answer(t, q, stepText, introText, reportText)
	for _, tc := range []struct {
		name  string
		given string
		short bool
	}{
		{"lost", model.LostAnswer, false},
		{"wrong", "nothing here", false},
		{"right", right, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			second.BeginCall(tc.given, nil)
			for p, text := range map[engine.Piece]string{stepLane: stepText, introLane: introText, reportLane: reportText} {
				if got := serve(second.Session, p, text); got != tc.short {
					t.Errorf("%s should serve short only on a right answer, whichever session served it", p.Name)
				}
			}
		})
	}
}

func TestLinksChain(t *testing.T) {
	first := newLoggedSession(t, "s_first")
	_, _ = respond(t, first.Session, "", map[engine.Piece]string{stepLane: stepText})
	second := linkTo(t, "s_second", first)
	_, _ = respond(t, second.Session, "", map[engine.Piece]string{introLane: introText})
	third := linkTo(t, "s_third", second)

	_, q := respond(t, third.Session, "", map[engine.Piece]string{reportLane: reportText})
	if name := questionName(t, q); name != stepLane.Name {
		t.Fatalf("a chained link should carry the first session's texts as the oldest, got %q", q)
	}
	third.BeginCall(answer(t, q, stepText, introText, reportText), nil)
	if !serve(third.Session, stepLane, stepText) || !serve(third.Session, introLane, introText) {
		t.Error("a session linking a session that links a third should carry the texts of both")
	}
}

func TestALinkSurvivesARestart(t *testing.T) {
	first := newLoggedSession(t, "s_first")
	_, _ = respond(t, first.Session, "", map[engine.Piece]string{stepLane: stepText})
	second := linkTo(t, "s_second", first)
	_, q := respond(t, second.Session, "", map[engine.Piece]string{introLane: introText})

	replayed, err := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).
		ReplaySession("s_second", "tester", second.events(t), func(string) (*engine.Spec, error) { return nil, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	replayed.BeginCall(answer(t, q, stepText, introText), nil)
	if !serve(replayed, stepLane, stepText) {
		t.Error("a restarted linked session should still hold the carried texts on a right answer")
	}
}

func TestALinkCarriesWhatTheAgentWrote(t *testing.T) {
	spec, err := engine.ParseSpec(echoEntry(t))
	if err != nil {
		t.Fatal(err)
	}
	first := newLoggedSession(t, "s_first")
	sv, err := first.Start(spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Report(sv.Instance, map[string]any{"note": reportText}); err != nil {
		t.Fatal(err)
	}
	draft := engine.Piece{Name: "lane draft", Hash: "d1"}
	first.BeginCall("", nil)
	first.ServeAuthored(draft, introText, introText)
	first.RecordServed()

	second := linkTo(t, "s_second", first)
	_, q := respond(t, second.Session, "", map[engine.Piece]string{reportLane: reportText})
	if q != "" {
		t.Errorf("a linked session should ask about no carried text the agent wrote, nor a phrase it reported, got %q", q)
	}
}

func TestHeldTexts(t *testing.T) {
	t.Run("leaves out texts served before the held set emptied", func(t *testing.T) {
		session := newLoggedSession(t, "s_reset")
		_, _ = respond(t, session.Session, "", map[engine.Piece]string{introLane: introText})
		_, _ = respond(t, session.Session, model.LostAnswer, map[engine.Piece]string{stepLane: stepText})
		carried, recorded := engine.HeldTexts(session.events(t))
		if !recorded || len(carried) != 1 || carried[0].Name != stepLane.Name || carried[0].Text != stepText {
			t.Errorf("only texts served since lost should carry, got %+v", carried)
		}
	})
	t.Run("orders by latest copy", func(t *testing.T) {
		session := newLoggedSession(t, "s_order")
		_, q := respond(t, session.Session, "", map[engine.Piece]string{stepLane: stepText})
		_, q = respond(t, session.Session, answer(t, q, stepText), map[engine.Piece]string{introLane: introText})
		session.BeginCall(answer(t, q, stepText, introText), []string{stepLane.Name})
		serve(session.Session, stepLane, stepText)
		session.RecordServed()
		carried, _ := engine.HeldTexts(session.events(t))
		if len(carried) != 2 || carried[0].Name != introLane.Name || carried[1].Name != stepLane.Name {
			t.Errorf("carried texts should run from the oldest latest copy, got %+v", carried)
		}
	})
	for name, log := range map[string]string{
		"a stock log":                        `{"v":1,"seq":1,"event":"served_blocks","data":{"hashes":["abc"]}}`,
		"a log from before the question":     `{"v":1,"seq":1,"event":"anchors","data":{"anchors":[{"name":"lane old","tag":"ab12","word":"draft"}]}}`,
		"a log of framing served per change": `{"v":1,"seq":1,"event":"texts","data":{"texts":[{"name":"framing x","hash":"h","changed":true}]}}`,
	} {
		t.Run(name+" holds no record", func(t *testing.T) {
			events, err := engine.ReadEvents(bytes.NewBufferString(log + "\n"))
			if err != nil {
				t.Fatal(err)
			}
			if _, recorded := engine.HeldTexts(events); recorded {
				t.Error("a log without served texts should hold no record of them")
			}
		})
	}
}
