package engine_test

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// startMove starts a one-step move in session, whose serves pieces can
// belong to.
func startMove(t *testing.T, session *engine.Session) string {
	t.Helper()
	spec, err := engine.LoadSpec(laneEntry(t, laneMachine, "## unit: draft\n\nDraft the note."), engine.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	serve, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return serve.Instance
}

func heldNames(line []engine.HeldPiece) []string {
	names := make([]string, 0, len(line))
	for _, held := range line {
		names = append(names, held.Name)
	}
	return names
}

func replayPieces(t *testing.T, log *bytes.Buffer) *engine.Session {
	t.Helper()
	events, err := engine.ReadEvents(bytes.NewReader(log.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	reg := engine.NewRegistry()
	spec, err := engine.LoadSpec(laneEntry(t, laneMachine, "## unit: draft\n\nDraft the note."), reg)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := engine.New(reg, engine.StaticGraphs{Graph: model.NewGraph(nil)}).
		ReplaySession("s_pieces", "tester", events, func(string) (*engine.Spec, error) { return spec, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	return replayed
}

// The hold-next line (20261114-121534-d-tac-qdb) names, by the tag each was
// last served under: the lanes and report schema of the step a running move
// stands at, the knowledge hints, the reading hints, then the rest the step
// carried and what the call served outside it.
func TestHoldNextNamesTheLikeliestPiecesFirst(t *testing.T) {
	framing := engine.Piece{Name: "framing principles", Hash: "h1"}
	lane := engine.Piece{Name: "lane draft", Hash: "h2"}
	schema := engine.Piece{Name: "lanesproc report fields", Hash: "h3"}
	overview := engine.Piece{Name: "20260812-180000-s-prc-typ"}
	read := engine.Piece{Name: "20260101-000000-s-tac-aaa"}
	shown := engine.Piece{Name: "20260101-000000-s-tac-bbb"}
	session := newPieceSession(nil)
	move := startMove(t, session)

	session.BeginCall(nil, nil)
	session.ServeFor(move)
	framingProof := servedInFullAs(t, session, framing, engine.HoldCarried)
	laneProof := servedInFullAs(t, session, lane, engine.HoldStep)
	servedInFullAs(t, session, schema, engine.HoldStep)
	session.RecordAnchors()

	session.BeginCall(nil, nil)
	for _, entry := range []engine.Piece{overview, read, shown} {
		servedInFull(t, session, entry)
	}
	session.RecordAnchors()
	hints := engine.HoldHints{Knowledge: []string{overview.Name}, Reading: []string{read.Name}}
	line := session.HoldNext(hints)
	want := []string{lane.Name, schema.Name, overview.Name, read.Name, framing.Name, shown.Name}
	if !slices.Equal(heldNames(line), want) {
		t.Fatalf("the line should run step, knowledge, reading, then carried pieces, got %v", heldNames(line))
	}
	if line[0].Tag != laneProof.Tag || line[4].Tag != framingProof.Tag {
		t.Errorf("each piece should be named by the tag it was served under, got %+v", line)
	}

	session.BeginCall([]model.Proof{laneProof}, nil)
	if got := heldNames(session.HoldNext(hints)); !slices.Equal(got, want[:5]) {
		t.Errorf("an entry served outside a move's step should leave the line with its call, got %v", got)
	}
}

// The line names at most engine.MaxHoldNext tags, so it is as long as what
// the next step can repeat and never longer.
func TestHoldNextNamesAtMostTheCap(t *testing.T) {
	session := newPieceSession(nil)
	move := startMove(t, session)
	session.BeginCall(nil, nil)
	session.ServeFor(move)
	var want []string
	for i := range engine.MaxHoldNext + 5 {
		lane := engine.Piece{Name: fmt.Sprintf("lane l%02d", i), Hash: "h"}
		servedInFullAs(t, session, lane, engine.HoldStep)
		want = append(want, lane.Name)
	}
	session.RecordAnchors()
	if got := heldNames(session.HoldNext(engine.HoldHints{})); !slices.Equal(got, want[:engine.MaxHoldNext]) {
		t.Errorf("the line should name the first %d lanes in serve order, got %v", engine.MaxHoldNext, got)
	}
}

// A step a move moves on to replaces what its last step carried, also after
// a restart; a second record in the same call, as the end of every call
// makes, keeps it.
func TestHoldNextFollowsTheStepAMoveStandsAt(t *testing.T) {
	first := engine.Piece{Name: "lane first", Hash: "h1"}
	second := engine.Piece{Name: "lane second", Hash: "h2"}
	var log bytes.Buffer
	session := newPieceSession(engine.NewWriterSink(&log))
	move := startMove(t, session)
	session.BeginCall(nil, nil)
	session.ServeFor(move)
	servedInFullAs(t, session, first, engine.HoldStep)
	session.RecordAnchors()
	session.BeginCall(nil, nil)
	session.ServeFor(move)
	servedInFullAs(t, session, second, engine.HoldStep)
	session.RecordAnchors()
	session.RecordAnchors()

	session.BeginCall(nil, nil)
	if got := heldNames(session.HoldNext(engine.HoldHints{})); !slices.Equal(got, []string{second.Name}) {
		t.Errorf("the line should name the lanes of the step the move stands at, got %v", got)
	}
	replayed := replayPieces(t, &log)
	replayed.BeginCall(nil, nil)
	if got := heldNames(replayed.HoldNext(engine.HoldHints{})); !slices.Equal(got, []string{second.Name}) {
		t.Errorf("a restarted server should name the same step's lanes, got %v", got)
	}
}

// A piece served in full where a proof could have held it joins the lines of
// its move until the move ends; one the agent asked for again in reread, or
// served once, does not.
func TestHoldNextNamesARepeatForTheRestOfItsMove(t *testing.T) {
	repeated := engine.Piece{Name: "20260101-000000-s-tac-aaa"}
	asked := engine.Piece{Name: "20260101-000000-s-tac-bbb"}
	once := engine.Piece{Name: "20260101-000000-s-tac-ccc"}
	var log bytes.Buffer
	session := newPieceSession(engine.NewWriterSink(&log))
	move := startMove(t, session)
	session.BeginCall(nil, nil)
	servedInFull(t, session, repeated)
	askedProof := servedInFull(t, session, asked)
	servedInFull(t, session, once)
	session.RecordAnchors()

	session.BeginCall(nil, []string{askedProof.Tag})
	servedInFull(t, session, repeated)
	servedInFull(t, session, asked)
	session.RecordAnchors()

	session.BeginCall(nil, nil)
	if got := heldNames(session.HoldNext(engine.HoldHints{})); !slices.Equal(got, []string{repeated.Name}) {
		t.Fatalf("only the piece served in full again unasked should stay on the line, got %v", got)
	}
	replayed := replayPieces(t, &log)
	replayed.BeginCall(nil, nil)
	if got := heldNames(replayed.HoldNext(engine.HoldHints{})); !slices.Equal(got, []string{repeated.Name}) {
		t.Errorf("a restarted server should keep naming the repeat, got %v", got)
	}

	if err := session.Abandon(move, "done"); err != nil {
		t.Fatal(err)
	}
	session.BeginCall(nil, nil)
	if got := session.HoldNext(engine.HoldHints{}); len(got) != 0 {
		t.Errorf("a repeat should leave the line once its move ended, got %v", heldNames(got))
	}
}

// A piece with no word to tag cannot be proved, so the line never names it.
func TestHoldNextLeavesOutUntaggedPieces(t *testing.T) {
	session := newPieceSession(nil)
	move := startMove(t, session)
	session.BeginCall(nil, nil)
	session.ServeFor(move)
	session.ServePiece(engine.Piece{Name: "lane bare", Hash: "h1"}, engine.HoldStep, nil, placeIn("— of a"))
	session.RecordAnchors()
	if got := session.HoldNext(engine.HoldHints{Reading: []string{"20260101-000000-s-tac-aaa"}}); len(got) != 0 {
		t.Errorf("neither an untagged piece nor an entry never served in full should be named, got %v", heldNames(got))
	}
}

// reviewMachine leads from review, through the draft gate, back to playback;
// aside is reached only from playback.
const reviewMachine = `state:
    body: {type: text, desc: x}
steps:
    - id: draft
      collect: [body]
      transitions:
          - when: hasBody
            to: playback
    - id: playback
      chooser: user
      options:
          - {choice: confirm, to: review}
          - {choice: detour, to: aside}
    - id: aside
      chooser: agent
      options:
          - {choice: back, to: playback}
    - id: review
      chooser: agent
      options:
          - {choice: revise, collect: ["body?"], to: draft}
          - {choice: proceed, to: end(completed)}`

// The line names, after the step a move stands at, the steps its options
// lead to, on through steps that advance on their own, nearest first, also
// after a restart; a step no option reaches stays off.
func TestHoldNextNamesTheStepsTheOptionsLeadTo(t *testing.T) {
	reg := engine.NewRegistry()
	spec, err := engine.LoadSpec(laneEntry(t, reviewMachine, "## unit: draft\n\nDraft.\n\n## unit: playback\n\nPlay back.\n\n## unit: aside\n\nAside.\n\n## unit: review\n\nReview."), reg)
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	session := newPieceSession(engine.NewWriterSink(&log))
	serve, err := session.Start(spec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	move := serve.Instance
	// Each step serves its lane, as its pointer once the agent proves it, so
	// no lane is a repeat the line would name anyway.
	proofs := map[string]model.Proof{}
	serveStep := func(lane string) {
		t.Helper()
		piece := engine.Piece{Name: "lane " + lane, Hash: lane}
		proof, served := proofs[lane]
		if served {
			session.BeginCall([]model.Proof{proof}, nil)
		} else {
			session.BeginCall(nil, nil)
		}
		session.ServeFor(move)
		if served {
			if got := session.ServePiece(piece, engine.HoldStep, nil, placeIn(pieceText)); !got.Held {
				t.Fatalf("the proved %s lane should serve as its pointer, got %+v", lane, got)
			}
		} else {
			proofs[lane] = servedInFullAs(t, session, piece, engine.HoldStep)
		}
		session.RecordAnchors()
	}
	advance := func(advanced *engine.Serve, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		serveStep(advanced.Step)
	}
	serveStep("draft")
	advance(session.Report(move, map[string]any{"body": "x"}))
	advance(session.Answer(move, "playback", "detour", nil, "aside"))
	advance(session.Answer(move, "aside", "back", nil, ""))
	advance(session.Answer(move, "playback", "confirm", nil, "yes"))

	want := []string{"lane review", "lane draft", "lane playback"}
	session.BeginCall(nil, nil)
	if got := heldNames(session.HoldNext(engine.HoldHints{})); !slices.Equal(got, want) {
		t.Errorf("the line should name the review step, then the draft gate and the playback its revise leads to, got %v", got)
	}
	events, err := engine.ReadEvents(bytes.NewReader(log.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := engine.New(reg, engine.StaticGraphs{Graph: model.NewGraph(nil)}).
		ReplaySession("s_pieces", "tester", events, func(string) (*engine.Spec, error) { return spec, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	replayed.BeginCall(nil, nil)
	if got := heldNames(replayed.HoldNext(engine.HoldHints{})); !slices.Equal(got, want) {
		t.Errorf("a restarted server should name the same steps, got %v", got)
	}
}
