package engine_test

import (
	"bytes"
	"fmt"
	"slices"
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

func unprovedNames(list []engine.UnprovedPiece) []string {
	names := make([]string, 0, len(list))
	for _, p := range list {
		names = append(names, p.Name)
	}
	return names
}

// The unproved list (20261113-121039-d-tac-ca2) names the pieces earlier
// calls served in full that no call proved since, newest first, at most
// MaxUnprovedPieces of them; the call's own full serves are not on it.
func TestUnprovedListsEarlierPiecesNewestFirst(t *testing.T) {
	session := newPieceSession(nil)
	session.BeginCall(nil, nil)
	var tags []string
	for i := range engine.MaxUnprovedPieces + 5 {
		proof := servedInFull(t, session, engine.Piece{Name: fmt.Sprintf("lane l%02d", i), Hash: "h"})
		tags = append(tags, proof.Tag)
	}
	if listed := session.Unproved(); len(listed) != 0 {
		t.Errorf("pieces the call serves in full should not be listed in its own result, got %v", unprovedNames(listed))
	}
	session.RecordAnchors()
	if listed := session.Unproved(); len(listed) != 0 {
		t.Errorf("pieces the call recorded should not be listed in its own result either, got %v", unprovedNames(listed))
	}

	session.BeginCall(nil, nil)
	listed := session.Unproved()
	if len(listed) != engine.MaxUnprovedPieces {
		t.Fatalf("the list should hold the newest %d pieces, got %d", engine.MaxUnprovedPieces, len(listed))
	}
	newest := len(tags) - 1
	if listed[0].Name != fmt.Sprintf("lane l%02d", newest) || listed[0].Tag != tags[newest] {
		t.Errorf("the list should open with the newest piece under its tag, got %+v", listed[0])
	}
	if last := listed[len(listed)-1]; last.Name != "lane l05" {
		t.Errorf("the list should end with the oldest piece within the cap, got %+v", last)
	}
}

func TestUnprovedDropsWhatACallProvesOrServesAgain(t *testing.T) {
	proved := engine.Piece{Name: "lane proved", Hash: "h1"}
	again := engine.Piece{Name: "lane again", Hash: "h2"}
	wrong := engine.Piece{Name: "lane wrong", Hash: "h3"}
	entry := engine.Piece{Name: "20260101-000000-s-tac-aaa"}
	framing := engine.Piece{Name: "framing principles", Hash: "h4"}
	var log bytes.Buffer
	session := newPieceSession(engine.NewWriterSink(&log))
	session.BeginCall(nil, nil)
	proof := servedInFull(t, session, proved)
	servedInFull(t, session, again)
	wrongProof := servedInFull(t, session, wrong)
	servedInFull(t, session, entry)
	framingProof := servedInFull(t, session, framing, entry.Name)
	session.RecordAnchors()

	session.BeginCall([]model.Proof{proof, framingProof, {Tag: wrongProof.Tag, Word: "elsewhere"}}, nil)
	servedInFull(t, session, again)
	if got := unprovedNames(session.Unproved()); !slices.Equal(got, []string{wrong.Name}) {
		t.Errorf("only the piece neither proved nor served again should be listed, got %v", got)
	}
	servedInFull(t, session, wrong)
	session.RecordAnchors()

	session.BeginCall(nil, nil)
	if got := unprovedNames(session.Unproved()); !slices.Equal(got, []string{wrong.Name, again.Name}) {
		t.Errorf("pieces served in full again should be listed under their new tags, the proved ones not, got %v", got)
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
	replayed.BeginCall(nil, nil)
	if got := unprovedNames(replayed.Unproved()); !slices.Equal(got, []string{wrong.Name, again.Name}) {
		t.Errorf("a restarted server should list what the session left unproved, got %v", got)
	}
}

// A piece with no word to tag cannot be proved, so it is never listed.
func TestUnprovedLeavesOutUntaggedPieces(t *testing.T) {
	session := newPieceSession(nil)
	session.BeginCall(nil, nil)
	session.ServePiece(engine.Piece{Name: "lane bare", Hash: "h1"}, nil, placeIn("— of a"))
	session.RecordAnchors()
	session.BeginCall(nil, nil)
	if listed := session.Unproved(); len(listed) != 0 {
		t.Errorf("an untagged piece should not be listed, got %v", unprovedNames(listed))
	}
}

// A proof is recorded only while it takes a piece off the list, so proving
// what is already off it writes nothing.
func TestUnprovedRecordsOnlyProofsThatChangeTheList(t *testing.T) {
	lane := engine.Piece{Name: "lane step", Hash: "h1"}
	var log bytes.Buffer
	session := newPieceSession(engine.NewWriterSink(&log))
	session.BeginCall(nil, nil)
	proof := servedInFull(t, session, lane)
	session.RecordAnchors()

	session.BeginCall([]model.Proof{proof}, nil)
	session.RecordAnchors()
	recorded := log.Len()
	session.BeginCall([]model.Proof{proof}, nil)
	session.RecordAnchors()
	if log.Len() != recorded {
		t.Errorf("a proof of a piece already off the list should record nothing, got %q", log.String()[recorded:])
	}
}
