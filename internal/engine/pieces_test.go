package engine_test

import (
	"bytes"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

const pieceText = "Draft one entry, dialogue first, then check its grounding against the graph."

func placeIn(text string) engine.AnchorPlacer {
	return func(tag string, rnd model.RandomSource) (model.Anchor, bool) {
		return model.PlaceAnchor(text, tag, rnd)
	}
}

func newPieceSession(sink engine.EventSink) *engine.Session {
	return engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).NewSession("s_pieces", "tester", sink)
}

func servedInFull(t *testing.T, session *engine.Session, piece engine.Piece, covers ...engine.Piece) model.Proof {
	t.Helper()
	served := session.ServePiece(piece, covers, placeIn(pieceText))
	if served.Held || served.Anchor.Tag == "" {
		t.Fatalf("the piece should serve in full under a fresh tag, got %+v", served)
	}
	return model.Proof{Tag: served.Anchor.Tag, Word: served.Anchor.Word}
}

func TestServePieceServesInFullUntilTheCallProvesIt(t *testing.T) {
	lane := engine.Piece{Name: "lane step", Hash: "h1"}
	session := newPieceSession(nil)

	session.BeginCall(nil, nil)
	proof := servedInFull(t, session, lane)
	if served := session.ServePiece(lane, nil, placeIn(pieceText)); !served.Held || served.Tag != proof.Tag {
		t.Errorf("a repeat within the same call should point at the tag just served, got %+v", served)
	}
	session.RecordAnchors()

	session.BeginCall(nil, nil)
	if again := servedInFull(t, session, lane); again.Tag == proof.Tag {
		t.Error("a call without the proof should get the piece in full under a fresh tag")
	}

	session.BeginCall([]model.Proof{proof}, nil)
	if served := session.ServePiece(lane, nil, placeIn(pieceText)); !served.Held || served.Tag != proof.Tag {
		t.Errorf("a call proving the piece should get its pointer, got %+v", served)
	}
}

func TestServePieceRefusesProofsThatDoNotMatch(t *testing.T) {
	lane := engine.Piece{Name: "lane step", Hash: "h1"}
	other := engine.Piece{Name: "lane intro", Hash: "h2"}
	session := newPieceSession(nil)
	session.BeginCall(nil, nil)
	proof := servedInFull(t, session, lane)
	otherProof := servedInFull(t, session, other)
	session.RecordAnchors()

	cases := []struct {
		name   string
		held   []model.Proof
		reread []string
		piece  engine.Piece
	}{
		{name: "wrong word", held: []model.Proof{{Tag: proof.Tag, Word: "elsewhere"}}, piece: lane},
		{name: "never issued", held: []model.Proof{{Tag: "zzzz", Word: proof.Word}}, piece: lane},
		{name: "another piece's proof", held: []model.Proof{otherProof}, piece: lane},
		{name: "changed content", held: []model.Proof{proof}, piece: engine.Piece{Name: lane.Name, Hash: "h1-changed"}},
		{name: "reread by tag", held: []model.Proof{proof}, reread: []string{"⟦" + proof.Tag + "⟧"}, piece: lane},
		{name: "reread by name", held: []model.Proof{proof}, reread: []string{"Lane Step"}, piece: lane},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session.BeginCall(tc.held, tc.reread)
			servedInFull(t, session, tc.piece)
		})
	}
}

func TestServePieceHoldsTheEntriesAProvedPieceCarried(t *testing.T) {
	framing := engine.Piece{Name: "framing principles", Hash: "h1"}
	carried := engine.Piece{Name: "20260101-000000-s-tac-aaa"}
	session := newPieceSession(nil)
	session.BeginCall(nil, nil)
	proof := servedInFull(t, session, framing, carried)
	if _, held := session.Holds(carried); !held {
		t.Error("an entry the piece just served carries should be held within the call")
	}
	session.RecordAnchors()

	session.BeginCall(nil, nil)
	if _, held := session.Holds(carried); held {
		t.Error("without the carrying piece's proof the entry should serve in full")
	}
	session.BeginCall([]model.Proof{proof}, nil)
	if tag, held := session.Holds(carried); !held || tag != proof.Tag {
		t.Errorf("the carrying piece's proof should hold the entry under its tag, got %q %v", tag, held)
	}
	if _, held := session.Holds(engine.Piece{Name: "20260101-000000-s-tac-bbb"}); held {
		t.Error("an entry the piece did not carry should not be held")
	}
	if _, held := session.Holds(engine.Piece{Name: carried.Name, Hash: "edited"}); held {
		t.Error("an entry edited since the piece carried it should not be held")
	}
}

func TestReorientServesTheCallInFull(t *testing.T) {
	lane := engine.Piece{Name: "lane step", Hash: "h1"}
	session := newPieceSession(nil)
	session.BeginCall(nil, nil)
	proof := servedInFull(t, session, lane)
	session.RecordAnchors()

	session.BeginCall([]model.Proof{proof}, nil)
	session.Reorient()
	servedInFull(t, session, lane)
}

func TestAnchorsSurviveARestart(t *testing.T) {
	lane := engine.Piece{Name: "lane step", Hash: "h1"}
	entry := engine.Piece{Name: "20260101-000000-s-tac-aaa"}
	var log bytes.Buffer
	session := newPieceSession(engine.NewWriterSink(&log))
	session.BeginCall(nil, nil)
	proof := servedInFull(t, session, lane, entry)
	session.RecordAnchors()
	log.WriteString(`{"v":1,"seq":99,"event":"served_blocks","data":{"hashes":["written-before-the-anchor-proof"]}}` + "\n")

	events, err := engine.ReadEvents(&log)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).
		ReplaySession("s_pieces", "tester", events, func(string) (*engine.Spec, error) { return nil, nil }, nil)
	if err != nil {
		t.Fatalf("replaying a log carrying anchors and an older served-blocks event: %v", err)
	}
	replayed.BeginCall([]model.Proof{proof}, nil)
	if served := replayed.ServePiece(lane, nil, placeIn(pieceText)); !served.Held {
		t.Error("a proof issued before the restart should still hold the piece")
	}
	if _, held := replayed.Holds(entry); !held {
		t.Error("a proof issued before the restart should still hold the entries its piece carried")
	}
}

func TestDropPiecesSinceWithdrawsWhatACutKeptFromTheAgent(t *testing.T) {
	kept := engine.Piece{Name: "lane step", Hash: "h1"}
	cut := engine.Piece{Name: "20260101-000000-s-tac-aaa"}
	session := newPieceSession(nil)
	session.BeginCall(nil, nil)
	servedInFull(t, session, kept)
	mark := session.PieceMark()
	servedInFull(t, session, cut)
	session.DropPiecesSince(mark)

	if _, held := session.Holds(cut); held {
		t.Error("a piece withdrawn after a cut should not count as served earlier in the call")
	}
	if _, held := session.Holds(kept); !held {
		t.Error("a piece served before the mark should stay served")
	}
}

// A pointer withdrawn before it reached the agent spends no proof; one
// served before the mark, or delivered, still does.
func TestAWithdrawnPointerSpendsNoProof(t *testing.T) {
	cases := []struct {
		name     string
		withdraw func(session *engine.Session, mark int, kept string)
	}{
		{name: "cut", withdraw: func(session *engine.Session, mark int, _ string) { session.DropPiecesSince(mark) }},
		{name: "left out of the result", withdraw: func(session *engine.Session, mark int, kept string) {
			session.KeepDelivered(mark, "a result holding only the pointer held as "+model.AnchorMark(kept))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first := engine.Piece{Name: "lane step", Hash: "h1"}
			second := engine.Piece{Name: "lane other", Hash: "h2"}
			session := newPieceSession(nil)
			session.BeginCall(nil, nil)
			firstProof, secondProof := servedInFull(t, session, first), servedInFull(t, session, second)
			session.RecordAnchors()

			session.BeginCall([]model.Proof{firstProof, secondProof}, nil)
			if !session.ServePiece(first, nil, placeIn(pieceText)).Held {
				t.Fatal("the proof should hold the first piece")
			}
			mark := session.PieceMark()
			if !session.ServePiece(second, nil, placeIn(pieceText)).Held {
				t.Fatal("the proof should hold the second piece")
			}
			tc.withdraw(session, mark, firstProof.Tag)
			session.RecordAnchors()

			session.BeginCall([]model.Proof{firstProof, secondProof}, nil)
			if _, held := session.Holds(first); held {
				t.Error("the pointer that reached the agent should have spent its proof")
			}
			if _, held := session.Holds(second); !held {
				t.Error("the withdrawn pointer should leave its proof unspent")
			}
		})
	}
}

// A piece with no word to tag serves in full every call: no proof can hold
// it, so none is recorded.
func TestServePieceWithoutAWordToTagServesInFull(t *testing.T) {
	piece := engine.Piece{Name: "lane bare", Hash: "h1"}
	session := newPieceSession(nil)
	for range 2 {
		session.BeginCall([]model.Proof{{Tag: "zzzz", Word: "of"}}, nil)
		served := session.ServePiece(piece, nil, placeIn("— of a"))
		if served.Held || served.Anchor.Tag != "" {
			t.Fatalf("a piece with no word to tag should serve in full untagged, got %+v", served)
		}
		session.RecordAnchors()
	}
}

// A proof a pointer rested on is spent: an agent that kept it past a
// compaction gets the piece in full, never another pointer, after a restart
// too.
func TestServePieceRefusesASpentProof(t *testing.T) {
	lane := engine.Piece{Name: "lane step", Hash: "h1"}
	var log bytes.Buffer
	session := newPieceSession(engine.NewWriterSink(&log))
	session.BeginCall(nil, nil)
	proof := servedInFull(t, session, lane)
	session.RecordAnchors()

	session.BeginCall([]model.Proof{proof}, nil)
	if served := session.ServePiece(lane, nil, placeIn(pieceText)); !served.Held {
		t.Fatalf("a call proving the piece should get its pointer, got %+v", served)
	}
	session.RecordAnchors()

	session.BeginCall([]model.Proof{proof}, nil)
	if served := session.ServePiece(lane, nil, placeIn(pieceText)); served.Held {
		t.Fatalf("a proof replayed after its pointer should serve the piece in full, got %+v", served)
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
	replayed.BeginCall([]model.Proof{proof}, nil)
	if _, held := replayed.Holds(lane); held {
		t.Error("a spent proof should stay spent after a restart")
	}
}

// Each pointer names the next proof, a challenge's prompt; its answer proves
// the piece once, and the piece serves in full once its challenges are spent.
func TestServePieceRotatesItsProofs(t *testing.T) {
	text := "Draft one entry, dialogue first, then check its grounding against the graph before the write lands."
	lane := engine.Piece{Name: "lane step", Hash: "h1"}
	var log bytes.Buffer
	session := newPieceSession(engine.NewWriterSink(&log))
	session.BeginCall(nil, nil)
	served := session.ServePiece(lane, nil, placeIn(text))
	if len(served.Anchor.Challenges) == 0 {
		t.Fatal("a piece served in full should carry later challenges")
	}
	session.RecordAnchors()
	proof := model.Proof{Tag: served.Anchor.Tag, Word: served.Anchor.Word}

	for _, challenge := range served.Anchor.Challenges {
		session.BeginCall([]model.Proof{proof}, nil)
		pointed := session.ServePiece(lane, nil, placeIn(text))
		if !pointed.Held || pointed.Next != challenge.Prompt {
			t.Fatalf("a proved piece should serve its pointer naming the next challenge, got %+v", pointed)
		}
		if again := session.ServePiece(lane, nil, placeIn(text)); again.Next != challenge.Prompt {
			t.Fatalf("a second pointer in the same call should name the same challenge, got %+v", again)
		}
		session.RecordAnchors()
		proof = model.Proof{Tag: proof.Tag, Word: challenge.Answer}
	}

	session.BeginCall([]model.Proof{proof}, nil)
	if last := session.ServePiece(lane, nil, placeIn(text)); !last.Held || last.Next != "" {
		t.Fatalf("the last challenge's proof should earn a pointer naming no next proof, got %+v", last)
	}
	session.RecordAnchors()

	events, err := engine.ReadEvents(&log)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)}).
		ReplaySession("s_pieces", "tester", events, func(string) (*engine.Spec, error) { return nil, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	replayed.BeginCall([]model.Proof{proof}, nil)
	if _, held := replayed.Holds(lane); held {
		t.Error("a piece whose challenges are spent should serve in full, after a restart too")
	}
	session.BeginCall([]model.Proof{proof}, nil)
	servedInFull(t, session, lane)
}

// A proof that holds a piece the call serves no pointer for stays unspent.
func TestHoldsSpendsNoProof(t *testing.T) {
	lane := engine.Piece{Name: "lane step", Hash: "h1"}
	session := newPieceSession(nil)
	session.BeginCall(nil, nil)
	proof := servedInFull(t, session, lane)
	session.RecordAnchors()
	for range 2 {
		session.BeginCall([]model.Proof{proof}, nil)
		if _, held := session.Holds(lane); !held {
			t.Fatal("a proof only checked should keep holding the piece")
		}
		session.RecordAnchors()
	}
}

// A call that records mid-way still holds by the proof it began with, and
// spends it once.
func TestAProofSpentMidCallHoldsForTheRestOfIt(t *testing.T) {
	text := "Draft one entry, dialogue first, then check its grounding against the graph before the write lands."
	lane := engine.Piece{Name: "lane step", Hash: "h1"}
	session := newPieceSession(nil)
	session.BeginCall(nil, nil)
	served := session.ServePiece(lane, nil, placeIn(text))
	session.RecordAnchors()
	if len(served.Anchor.Challenges) == 0 {
		t.Fatal("a piece served in full should carry later challenges")
	}
	proof := model.Proof{Tag: served.Anchor.Tag, Word: served.Anchor.Word}

	session.BeginCall([]model.Proof{proof}, nil)
	for range 2 {
		if pointed := session.ServePiece(lane, nil, placeIn(text)); !pointed.Held || pointed.Next != served.Anchor.Challenges[0].Prompt {
			t.Fatalf("the call's proof should hold the piece after a record mid-call, got %+v", pointed)
		}
		session.RecordAnchors()
	}

	session.BeginCall([]model.Proof{{Tag: proof.Tag, Word: served.Anchor.Challenges[0].Answer}}, nil)
	if _, held := session.Holds(lane); !held {
		t.Error("the first challenge's answer should prove the piece: the call spent its proof once")
	}
}
