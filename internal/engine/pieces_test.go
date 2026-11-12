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

func servedInFull(t *testing.T, session *engine.Session, piece engine.Piece, covers ...string) model.Proof {
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
	proof := servedInFull(t, session, framing, carried.Name)
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
	proof := servedInFull(t, session, lane, entry.Name)
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
