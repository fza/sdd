package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
)

// ServedPiece is how a call serves one piece under the text question
// (20261121-230157-d-tac-ym8): Held when the call holds it and gets its
// pointer, otherwise Text is the piece in full.
type ServedPiece struct {
	Held bool
	Text string
}

// BeginCall opens a tool call carrying answer, the agent's answer to a
// question a response asked, empty for a call that carries none, and the
// piece names it lists in reread.
func (w *WorkflowSession) BeginCall(answer string, reread []string) {
	w.session.BeginCall(answer, reread)
}

// Answered reports whether the call's answer answers an open question, so
// the texts it proves serve short.
func (w *WorkflowSession) Answered() bool { return w.session.Answered() }

// ServeText serves a piece of text named name. covers names the entries the
// text carries whole, held for the rest of the call.
func (w *WorkflowSession) ServeText(name, text string, covers []string) ServedPiece {
	if w.session.ServePiece(engine.Piece{Name: name, Hash: contentHash(text)}, w.entryPieces(covers), text) {
		return ServedPiece{Held: true}
	}
	return ServedPiece{Text: text}
}

// ServeSchema serves a report schema named name, reporting whether the call
// holds it; a question about it asks about its descriptions.
func (w *WorkflowSession) ServeSchema(name string, schema map[string]any) (bool, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return false, fmt.Errorf("encoding the %s report schema: %w", name, err)
	}
	return w.session.ServePiece(engine.Piece{Name: name, Hash: contentHash(string(raw))}, nil, model.SchemaDescriptions(schema)), nil
}

// ServeNotice returns a notice named name that rides on results as served
// once per change of its text: whole while the text differs from the last the
// session served, empty otherwise. A finished session serves it whole, as its
// last serve.
func (w *WorkflowSession) ServeNotice(name, text string) string {
	if w.Finished() {
		return text
	}
	hash := ""
	if text != "" {
		hash = contentHash(text)
	}
	if !w.session.ServeChanged(engine.Piece{Name: name, Hash: hash}) {
		return ""
	}
	return text
}

// HoldsEntry reports whether the call holds the entry in full: served
// earlier in the same call or carried whole by a piece served in it. hash is
// the entry's model.EntryHash. A right answer never holds an entry
// (20261121-230157-d-tac-ym8 leaves entries to a decision of their own).
func (w *WorkflowSession) HoldsEntry(id, hash string) bool {
	return w.session.HoldsEntry(engine.Piece{Name: id, Hash: hash})
}

// ServeEntry records an entry's rendered body served in full.
func (w *WorkflowSession) ServeEntry(id, hash, _ string) {
	w.session.ServeEntry(engine.Piece{Name: id, Hash: hash})
}

// DropAnswer keeps the call's answer from counting its question answered,
// for a call that failed.
func (w *WorkflowSession) DropAnswer() { w.session.DropAnswer() }

// RecordPieces logs the texts the call served and the question its answer
// answered. A finished session records nothing: its last serve is the one
// that ended it.
func (w *WorkflowSession) RecordPieces(ctx context.Context, identity RequestIdentity) error {
	if w.Finished() {
		return nil
	}
	w.setOperation(ctx, identity)
	w.session.RecordServed()
	return w.session.SinkErr()
}

// Question returns the question a response ends with, about a text the
// session served, and logs it; empty for a finished session or when no text
// yields one.
func (w *WorkflowSession) Question(ctx context.Context, identity RequestIdentity) (string, error) {
	if w.Finished() {
		return "", nil
	}
	w.setOperation(ctx, identity)
	line := w.session.Ask()
	return line, w.session.SinkErr()
}

// servedInFull reports whether the consumer holds the entry in full: as
// HoldsEntry while CutRepeats is on, by the read ledger otherwise.
func (w *WorkflowSession) servedInFull(ctx *engine.Context, id string) bool {
	if w.cutRepeats {
		return w.session.HoldsEntry(w.entryPiece(id))
	}
	return ctx.Reads[id] == engine.ReadFull
}

// entryPiece is the piece an entry of the session's graph serves as, keyed
// by its stored text. An entry the graph does not hold keys on its ID alone.
func (w *WorkflowSession) entryPiece(id string) engine.Piece {
	g, err := w.graphs.Current()
	if err != nil {
		return engine.Piece{Name: id}
	}
	e, ok := g.ByID[id]
	if !ok {
		return engine.Piece{Name: id}
	}
	return engine.Piece{Name: id, Hash: model.EntryHash(e)}
}

func (w *WorkflowSession) entryPieces(ids []string) []engine.Piece {
	pieces := make([]engine.Piece, len(ids))
	for i, id := range ids {
		pieces[i] = w.entryPiece(id)
	}
	return pieces
}

// repeats is the no-repeat rule over the session's served memory while
// CutRepeats is on; nil otherwise, which serves everything in full.
func (w *WorkflowSession) repeats() *presenters.Repeats {
	if !w.cutRepeats {
		return nil
	}
	return &presenters.Repeats{Held: w.HoldsEntry, Served: w.ServeEntry}
}

func contentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
