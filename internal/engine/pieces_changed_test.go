package engine_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// A piece served once per change serves while its content differs from the
// last the session served, once within a call, and again when the call
// rereads it or serves everything in full.
func TestServeChangedServesOncePerChange(t *testing.T) {
	block := engine.Piece{Name: "framing principles", Hash: "h1"}
	session := newPieceSession(nil)

	session.BeginCall(nil, nil)
	if !session.Changed(block) || !session.ServeChanged(block) {
		t.Fatal("a block the session never served should serve")
	}
	if session.Changed(block) || session.ServeChanged(block) {
		t.Error("a block the call already served should not serve again in it")
	}
	session.RecordAnchors()

	session.BeginCall(nil, nil)
	if session.ServeChanged(block) {
		t.Error("an unchanged block should not serve again")
	}
	edited := engine.Piece{Name: block.Name, Hash: "h2"}
	if !session.ServeChanged(edited) {
		t.Error("a block whose content changed should serve again")
	}
	session.RecordAnchors()

	for _, tc := range []struct {
		name   string
		reread []string
		full   bool
	}{
		{name: "reread by name", reread: []string{"Framing Principles"}},
		{name: "reorientation", full: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session.BeginCall(nil, tc.reread)
			if tc.full {
				session.Reorient()
			}
			if !session.Changed(edited) {
				t.Error("an unchanged block should serve again when the call asks for it")
			}
		})
	}
}

// Changed records nothing: a block it reports changed serves on the call
// that serves it.
func TestChangedRecordsNothing(t *testing.T) {
	block := engine.Piece{Name: "framing focus", Hash: "h1"}
	session := newPieceSession(nil)
	session.BeginCall(nil, nil)
	session.Changed(block)
	session.RecordAnchors()
	session.BeginCall(nil, nil)
	if !session.ServeChanged(block) {
		t.Error("a block only asked about should still serve")
	}
}

// A landing leaves out the pieces the session served in full before; a call
// rereading one, or serving everything in full, has it again.
func TestServedBeforeNamesPiecesServedInFull(t *testing.T) {
	lane := engine.Piece{Name: "lane junction", Hash: "h1"}
	session := newPieceSession(nil)
	session.BeginCall(nil, nil)
	if session.ServedBefore(lane) {
		t.Fatal("a piece never served should not count as served before")
	}
	servedInFull(t, session, lane)
	session.RecordAnchors()

	session.BeginCall(nil, nil)
	if !session.ServedBefore(lane) {
		t.Error("a piece served in full under a tag should count as served before, unproved")
	}
	if session.ServedBefore(engine.Piece{Name: lane.Name, Hash: "h2"}) {
		t.Error("a piece whose content changed should not count as served before")
	}
	session.BeginCall(nil, []string{"lane junction"})
	if session.ServedBefore(lane) {
		t.Error("a piece the call rereads should not count as served before")
	}
	session.BeginCall(nil, nil)
	session.Reorient()
	if session.ServedBefore(lane) {
		t.Error("a reorientation should serve every piece in full")
	}
}

// A call reports whether it served a piece, in full or as its pointer, so a
// result serving none carries no unproved list.
func TestServesPiecesReportsWhatTheCallServed(t *testing.T) {
	lane := engine.Piece{Name: "lane step", Hash: "h1"}
	session := newPieceSession(nil)
	session.BeginCall(nil, nil)
	if session.ServesPieces() {
		t.Fatal("a call that served nothing should report none")
	}
	proof := servedInFull(t, session, lane)
	if !session.ServesPieces() {
		t.Error("a call serving a piece in full should report it")
	}
	session.RecordAnchors()

	session.BeginCall([]model.Proof{proof}, nil)
	session.ServeChanged(engine.Piece{Name: "sync notice", Hash: "n1"})
	if session.ServesPieces() {
		t.Error("a notice served once per change is no piece")
	}
	session.Point(lane)
	if !session.ServesPieces() {
		t.Error("a call serving a piece's pointer should report it")
	}
}

// UnprovedExcept leaves out the pieces it skips before the cap, so the list
// fills with the rest.
func TestUnprovedExceptSkipsBeforeTheCap(t *testing.T) {
	session := newPieceSession(nil)
	session.BeginCall(nil, nil)
	for i := range engine.MaxUnprovedPieces {
		servedInFull(t, session, engine.Piece{Name: "20260101-000000-s-tac-" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "x", Hash: "e"})
	}
	servedInFull(t, session, engine.Piece{Name: "lane old", Hash: "h"})
	session.RecordAnchors()
	session.BeginCall(nil, nil)
	for i := range engine.MaxUnprovedPieces {
		servedInFull(t, session, engine.Piece{Name: "20260102-000000-s-tac-" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "y", Hash: "e"})
	}
	session.RecordAnchors()

	session.BeginCall(nil, nil)
	listed := unprovedNames(session.UnprovedExcept(func(name string) bool { return strings.HasPrefix(name, "2026") }))
	if !slices.Equal(listed, []string{"lane old"}) {
		t.Errorf("the skipped pieces should leave room for the rest, got %v", listed)
	}
}
