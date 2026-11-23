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
	return session.ServePiece(p, nil, text, text)
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

// answer answers the questions a response ended with from texts as an agent
// holding them would.
func answer(t *testing.T, question string, texts ...string) string {
	t.Helper()
	words, ok := test.AnswerAll(question, texts)
	if !ok {
		t.Fatalf("the questions should be answerable from the texts served, got %q", question)
	}
	return words
}

// names lists the texts a response's questions ask about.
func names(question string) []string {
	var out []string
	for _, q := range test.Questions(question) {
		out = append(out, q.Name)
	}
	return out
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
	if got := names(question); len(got) == 0 || got[0] != stepLane.Name {
		t.Errorf("the first response should ask about its own text, got %q", question)
	}
}

func TestTheFirstQuestionAsksAboutTheOldestTextByItsLatestCopy(t *testing.T) {
	session := newPieceSession(nil)
	_, q := respond(t, session, "", map[engine.Piece]string{stepLane: stepText})
	_, q = respond(t, session, answer(t, q, stepText), map[engine.Piece]string{introLane: introText})
	if got := names(q); got[0] != stepLane.Name {
		t.Errorf("the first question should ask about the text served longest ago, got %q", q)
	}
	// A reread serves the step text again, which makes intro the oldest copy.
	session.BeginCall(answer(t, q, stepText, introText), []string{stepLane.Name})
	serve(session, stepLane, stepText)
	session.RecordServed()
	q = session.Ask()
	if got := names(q); got[0] != introLane.Name {
		t.Errorf("a text served in full again should no longer be the oldest, got %q", q)
	}
}

// A right answer beside a wrong one proves the right part's text and every
// text newer than it, not the older text the wrong part asked about.
func TestARightAnswerBesideAWrongOneHoldsFromItsOwnText(t *testing.T) {
	session := newPieceSession(nil)
	_, q := respond(t, session, "", map[engine.Piece]string{stepLane: stepText})
	_, q = respond(t, session, answer(t, q, stepText), map[engine.Piece]string{introLane: introText})
	questions := test.Questions(q)
	if len(questions) != 2 || questions[0].Name != stepLane.Name || questions[1].Name != introLane.Name {
		t.Fatalf("the response should ask about the older step text first and the newer intro text second, got %q", q)
	}
	right := strings.Split(answer(t, q, stepText, introText), " ; ")

	session.BeginCall("nothing here ; "+right[1], nil)
	if serve(session, stepLane, stepText) {
		t.Error("the older text the wrong part asked about should serve in full")
	}
	if !serve(session, introLane, introText) {
		t.Error("the text the right part answers should serve short")
	}
	if session.Emptied() {
		t.Error("a right answer beside a wrong one should leave the held set standing")
	}
}

func TestAResponseAsksTwoQuestionsAboutTwoTexts(t *testing.T) {
	session := newPieceSession(nil)
	_, q := respond(t, session, "", map[engine.Piece]string{stepLane: stepText, introLane: introText})
	got := names(q)
	if len(got) != 2 || got[0] == got[1] {
		t.Errorf("a response should ask two questions about two different held texts, got %q", q)
	}
}

func TestOneTextYieldsTwoQuestionsAtTwoSpots(t *testing.T) {
	session := newPieceSession(nil)
	_, q := respond(t, session, "", map[engine.Piece]string{stepLane: stepText})
	questions := test.Questions(q)
	if len(questions) != 2 || questions[0].Name != stepLane.Name || questions[1].Name != stepLane.Name {
		t.Fatalf("a response holding one text should ask two questions about it, got %q", q)
	}
	if questions[0].Ask == questions[1].Ask {
		t.Errorf("the two questions should ask about two spots of the text, got %q", q)
	}
}

// Both answers right, or a right one beside a wrong one, serve short; lost
// to either, or every part wrong, serves in full
// (20261122-234358-d-tac-lv2 keeps lost as the compaction guard).
func TestARightAnswerServesShortUnlessAPartIsLost(t *testing.T) {
	pieces := map[engine.Piece]string{stepLane: stepText, introLane: introText}
	tests := []struct {
		name  string
		given func(right []string) string
		short bool
	}{
		{"both right", func(r []string) string { return r[0] + " ; " + r[1] }, true},
		{"swapped", func(r []string) string { return r[1] + " ; " + r[0] }, true},
		{"right about the oldest text, other wrong", func(r []string) string { return r[0] + " ; nothing here" }, true},
		{"both wrong", func([]string) string { return "nothing here ; nor here" }, false},
		{"one lost", func(r []string) string { return r[0] + " ; " + model.LostAnswer }, false},
		{"one of two", func(r []string) string { return r[0] }, false},
		{"lost", func([]string) string { return model.LostAnswer }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			session := newPieceSession(nil)
			_, q := respond(t, session, "", pieces)
			right := strings.Split(answer(t, q, stepText, introText), " ; ")
			if len(right) != 2 {
				t.Fatalf("the response should ask two questions, got %q", q)
			}
			held, q := respond(t, session, tc.given(right), pieces)
			for p := range pieces {
				if held[p] != tc.short {
					t.Errorf("%s should serve short only when no part is lost and one is right", p.Name)
				}
			}
			if tc.short {
				return
			}
			// The held set emptied: the texts of the response before count
			// as unheld, while the next round's own copies do.
			held, _ = respond(t, session, answer(t, q, stepText, introText), pieces)
			for p := range pieces {
				if !held[p] {
					t.Errorf("%s served in full after the reset should count as held again", p.Name)
				}
			}
		})
	}
}

func TestNoQuestionIsAskedTwice(t *testing.T) {
	session := newPieceSession(nil)
	asked := map[string]bool{}
	_, q := respond(t, session, "", map[engine.Piece]string{stepLane: stepText})
	for range 60 {
		for _, question := range test.Questions(q) {
			if asked[question.Ask] {
				t.Fatalf("a question should never be asked twice, got %q again", question.Ask)
			}
			asked[question.Ask] = true
		}
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
		parts := strings.Split(words, " ; ")
		for _, part := range parts {
			for _, earlier := range given {
				if (model.TextQuestion{Answer: earlier}).Answered(part) {
					t.Fatalf("an answer given before should answer no later question, got %q again for %q", part, q)
				}
			}
		}
		given = append(given, parts...)
		_, q = respond(t, session, words, map[engine.Piece]string{stepLane: stepText})
	}
}

func TestNoQuestionAsksAboutAnAuthoredText(t *testing.T) {
	session := newPieceSession(nil)
	draft := engine.Piece{Name: "lane draft", Hash: "d1"}
	session.BeginCall("", nil)
	session.ServeAuthored(draft, stepText, stepText)
	session.RecordServed()
	if q := session.Ask(); q != "" {
		t.Errorf("no question should ask about a text the agent wrote, got %q", q)
	}

	_, q := respond(t, session, "", map[engine.Piece]string{introLane: introText})
	for _, name := range names(q) {
		if name == draft.Name {
			t.Errorf("no question should ask about a text the agent wrote, got %q", q)
		}
	}
	// A draft served after the text asked about is held by the right answer.
	revised := engine.Piece{Name: "lane draft", Hash: "d2"}
	session.BeginCall(answer(t, q, introText), nil)
	session.ServeAuthored(revised, reportText, reportText)
	session.RecordServed()
	q = session.Ask()
	session.BeginCall(answer(t, q, introText), nil)
	if !session.ServeAuthored(revised, reportText, reportText) {
		t.Error("a held authored text should still serve as its pointer after a right answer")
	}
}

// echoSpec is a procedure whose report takes a note, the agent's own words.
const echoSpec = `state:
    note: {type: prose, desc: the agent's words}
    goal: {type: text, optional: true, desc: closer}
steps:
    - id: draft
      collect: [note, "goal?"]
      transitions:
          - when: hasGoal
            to: end(completed)`

func echoEntry(t *testing.T) *model.Entry {
	t.Helper()
	content := "---\ntype: decision\nlayer: prc\nkind: procedure\ncanonical: echoproc\n" + echoSpec + "\n---\n\necho procedure\n\n## unit: draft\n\nWrite a note.\n"
	entry, err := model.ParseEntry("20260826-140100-d-prc-ech.md", content)
	if err != nil {
		t.Fatalf("fixture entry: %v", err)
	}
	return entry
}

func TestNoQuestionPhraseOccursInWhatTheAgentReported(t *testing.T) {
	spec, err := engine.ParseSpec(echoEntry(t))
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	session := newPieceSession(engine.NewWriterSink(&log))
	sv, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// The agent's note quotes the step text, as a summary would quote it.
	if _, err := session.Report(sv.Instance, map[string]any{"note": "I keep this:\n" + strings.ToUpper(stepText)}); err != nil {
		t.Fatal(err)
	}
	_, q := respond(t, session, "", map[engine.Piece]string{stepLane: stepText})
	if q != "" {
		t.Errorf("no question's phrase should occur in what the agent reported, got %q", q)
	}

	events, err := engine.ReadEvents(&log)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).
		ReplaySession("s_pieces", "tester", events, func(string) (*engine.Spec, error) { return spec, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	replayed.BeginCall("", nil)
	if q := replayed.Ask(); q != "" {
		t.Errorf("after a restart, no question's phrase should occur in what the agent reported, got %q", q)
	}
}

func TestALostAnswerRightAfterAShortServeCountsALateCatch(t *testing.T) {
	var log bytes.Buffer
	session := newPieceSession(engine.NewWriterSink(&log))
	pieces := map[engine.Piece]string{stepLane: stepText, introLane: introText}
	catches := func() int {
		events, err := engine.ReadEvents(bytes.NewReader(log.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		return engine.LateCatches(events)
	}

	_, _ = respond(t, session, "", pieces)
	// lost after a full serve is no late catch.
	_, q := respond(t, session, model.LostAnswer, pieces)
	if got := catches(); got != 0 {
		t.Fatalf("a lost answer after a full serve should count no late catch, got %d", got)
	}
	held, _ := respond(t, session, answer(t, q, stepText, introText), pieces)
	if !held[stepLane] {
		t.Fatal("a right answer should serve short")
	}
	// The next call answers lost: the text the short serve leant on was gone.
	session.BeginCall(model.LostAnswer, nil)
	session.RecordServed()
	if got := catches(); got != 1 {
		t.Errorf("a lost answer right after a short serve should count one late catch, got %d", got)
	}
	session.BeginCall(model.LostAnswer, nil)
	session.RecordServed()
	if got := catches(); got != 1 {
		t.Errorf("a second lost answer in a row should count no further catch, got %d", got)
	}

	// Another caller's lost after a short serve shows no compaction.
	_, q = respond(t, session, "", pieces)
	held, _ = respond(t, session, answer(t, q, stepText, introText), pieces)
	if !held[stepLane] {
		t.Fatal("a right answer should serve short")
	}
	session.BeginCall(model.LostAnswer, nil)
	session.SetCaller("verifier")
	session.RecordServed()
	if got := catches(); got != 1 {
		t.Errorf("a lost answer of another caller should count no late catch, got %d", got)
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
	if session.ServeEntry(entry, "body") {
		t.Fatal("an entry should serve in full the first time")
	}
	if !session.ServeEntry(entry, "body") {
		t.Error("an entry served earlier in the same call should serve as its pointer")
	}
	serve(session, stepLane, stepText)
	session.RecordServed()
	q := session.Ask()

	session.BeginCall(answer(t, q, stepText), nil)
	if session.ServeEntry(entry, "body") {
		t.Error("a right answer should not hold an entry served in an earlier call")
	}
}

func TestACarriedEntryIsHeldForTheRestOfTheCall(t *testing.T) {
	session := newPieceSession(nil)
	session.BeginCall("", nil)
	session.ServePiece(stepLane, []engine.Piece{entry}, stepText, stepText)
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

func TestKeepDeliveredKeepsWhatTheResultHolds(t *testing.T) {
	session := newPieceSession(nil)
	session.BeginCall("", nil)
	mark := session.PieceMark()
	serve(session, stepLane, stepText)
	serve(session, introLane, introText)
	session.KeepDelivered(mark, `{"instructions":"`+stepText+`"}`)

	if !session.Holds(stepLane) {
		t.Error("a piece the delivered result holds should stay served")
	}
	if session.Holds(introLane) {
		t.Error("a piece the delivered result does not hold should be withdrawn")
	}
}

func TestADeferralHoldsWhatItSentWhateverTheAnswer(t *testing.T) {
	session := newPieceSession(nil)
	session.BeginCall("", nil)
	serve(session, stepLane, stepText)
	serve(session, introLane, introText)
	session.RecordDeferral("i_1", stepText)
	session.RecordServed()

	session.BeginCall(model.LostAnswer, nil)
	session.FollowDeferral("i_1")
	if !session.Holds(stepLane) {
		t.Error("a piece the deferral sent should be held by the call that follows it")
	}
	if session.Holds(introLane) {
		t.Error("a piece the deferral's result did not carry should not be held")
	}

	session.BeginCall(model.LostAnswer, []string{stepLane.Name})
	session.FollowDeferral("i_1")
	if session.Holds(stepLane) {
		t.Error("a piece the call rereads should serve in full despite the deferral")
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

func TestServeChangedServesEachCallerOncePerChange(t *testing.T) {
	block := engine.Piece{Name: "framing focus", Hash: "h1"}
	var log bytes.Buffer
	session := newPieceSession(engine.NewWriterSink(&log))
	session.BeginCall("", nil)
	if !session.ServeChanged(block, "") {
		t.Fatal("the first caller should be served a block it was never sent")
	}
	session.RecordServed()

	session.BeginCall("", nil)
	if session.ServeChanged(block, "") {
		t.Error("the first caller should not be served an unchanged block again")
	}
	session.BeginCall("", nil)
	session.SetCaller("verifier")
	if !session.ServeChanged(block, "") {
		t.Error("a second caller the session never served should get the block")
	}
	session.RecordServed()

	events, err := engine.ReadEvents(&log)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).
		ReplaySession("s_pieces", "tester", events, func(string) (*engine.Spec, error) { return nil, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, caller := range []string{"", "verifier"} {
		replayed.BeginCall("", nil)
		replayed.SetCaller(caller)
		if replayed.ServeChanged(block, "") {
			t.Errorf("caller %q should hold the block after a restart", caller)
		}
	}
	replayed.BeginCall("", nil)
	if !replayed.ServeChanged(engine.Piece{Name: block.Name, Hash: "h2"}, "") {
		t.Error("a changed block should serve again")
	}
	replayed.BeginCall("", nil)
	replayed.SetCaller("third")
	if !replayed.ServeChanged(block, "") {
		t.Error("a third caller should get the block")
	}
}

// Emptied tells a call whose answer, lost or wrong, emptied the held set
// from one a right answer or no answer leaves standing.
func TestEmptiedNamesTheAnswersThatEmptyTheHeldSet(t *testing.T) {
	session := newPieceSession(nil)
	_, q := respond(t, session, "", map[engine.Piece]string{stepLane: stepText})
	right := answer(t, q, stepText)
	tests := []struct {
		name    string
		given   string
		emptied bool
	}{
		{"none", "", false},
		{"right", right, false},
		{"lost", model.LostAnswer, true},
		{"wrong", "nothing here", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session.BeginCall(tt.given, nil)
			switch {
			case tt.emptied && !session.Emptied():
				t.Error("the answer should empty the held set")
			case !tt.emptied && session.Emptied():
				t.Error("the answer should leave the held set standing")
			}
		})
	}
}

// A call that fails leaves the questions it answered open, and says so to
// an agent whose call carried an answer.
func TestAFailedCallSaysItsQuestionsStand(t *testing.T) {
	session := newPieceSession(nil)
	session.BeginCall("", nil)
	serve(session, stepLane, stepText)
	session.RecordServed()
	q := session.Ask()

	session.BeginCall("", nil)
	if session.QuestionsStand() {
		t.Error("a call that carried no answer should say nothing of the questions")
	}
	for _, given := range []string{answer(t, q, stepText), model.LostAnswer} {
		session.BeginCall(given, nil)
		if !session.QuestionsStand() {
			t.Errorf("a call answering %q should report its questions still open", given)
		}
	}
	session.BeginCall(answer(t, q, stepText), nil)
	session.RecordServed()
	session.BeginCall(model.LostAnswer, nil)
	if session.QuestionsStand() {
		t.Error("no question should stand once every round is answered")
	}
}
