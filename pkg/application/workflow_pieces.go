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
	// An entry piece keys on the entry's current text (entryPiece), so the
	// call reads the graph as it stands, edits since the last call included.
	if w.graphs != nil {
		w.graphs.Invalidate()
	}
}

// Answered reports whether the call's answer answers an open question, so
// the texts it proves serve short.
func (w *WorkflowSession) Answered() bool { return w.session.Answered() }

// ServeText serves a piece of text named name. covers names the entries the
// text carries whole, held for the rest of the call.
func (w *WorkflowSession) ServeText(name, text string, covers []string) ServedPiece {
	neutral := w.neutral(text)
	held := w.session.ServePiece(engine.Piece{Name: name, Hash: contentHash(neutral)}, w.entryPieces(covers), neutral, jsonEscaped(text))
	if held {
		return ServedPiece{Held: true}
	}
	return ServedPiece{Text: text}
}

// ServeAuthoredText is ServeText for text the agent or the person wrote,
// which no question asks about.
func (w *WorkflowSession) ServeAuthoredText(name, text string) ServedPiece {
	neutral := w.neutral(text)
	if w.session.ServeAuthored(engine.Piece{Name: name, Hash: contentHash(neutral)}, neutral, jsonEscaped(text)) {
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
	return w.session.ServePiece(engine.Piece{Name: name, Hash: contentHash(w.neutral(string(raw)))}, nil, w.neutral(model.SchemaDescriptions(schema)), string(raw)), nil
}

// ServeMark marks the pieces the call has served so far.
func (w *WorkflowSession) ServeMark() int { return w.session.PieceMark() }

// WithdrawServedSince withdraws the pieces served after mark, for a result
// composed again before it reaches the agent.
func (w *WorkflowSession) WithdrawServedSince(mark int) { w.session.DropPiecesSince(mark) }

// FollowDeferral makes the call follow the instance's deferral: what its
// last serve sent counts as held. Call it before the call advances the
// instance.
func (w *WorkflowSession) FollowDeferral(instance string) {
	w.session.FollowDeferral(instance)
}

// Resent reports whether the deferral the call follows sent the piece named
// name at the instance's step, when the call would serve text, its content
// now, in full all the same.
func (w *WorkflowSession) Resent(instance, name, text string) bool {
	return w.session.Resent(instance, engine.Piece{Name: name, Hash: contentHash(w.neutral(text))})
}

// EndDeferral ends the instance's deferral, as a serve of it settles it.
func (w *WorkflowSession) EndDeferral(instance string) {
	w.session.EndDeferral(instance)
}

// RecordDeferral records that the call's serve of the instance left lanes
// waiting past the result bound, with every piece the call served that
// delivered, the result's text, holds.
func (w *WorkflowSession) RecordDeferral(instance, delivered string) {
	w.session.RecordDeferral(instance, delivered)
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
	if !w.session.ServeChanged(engine.Piece{Name: name, Hash: hash}, w.neutral(text)) {
		return ""
	}
	return text
}

// SetCaller names the caller of the call in flight, the mark that tells a
// second caller on the session from the first; empty is the first.
func (w *WorkflowSession) SetCaller(caller string) { w.session.SetCaller(caller) }

// ServeOncePerChange reports whether text, served once per change under name,
// serves to the call's caller, and records that it does: its content differs
// from the last this caller was served under name, or the call serves
// everything in full.
func (w *WorkflowSession) ServeOncePerChange(name, text string) bool {
	return w.session.ServeChanged(engine.Piece{Name: name, Hash: contentHash(text)}, w.neutral(text))
}

// ForgetOncePerChange records that the call's caller holds no text under
// name, so the next call serves it whatever it holds.
func (w *WorkflowSession) ForgetOncePerChange(name string) {
	w.session.ServeChanged(engine.Piece{Name: name}, "")
}

// OncePerChangeDue is ServeOncePerChange recording nothing.
func (w *WorkflowSession) OncePerChangeDue(name, text string) bool {
	return w.session.Changed(engine.Piece{Name: name, Hash: contentHash(text)})
}

// HoldsEntry reports whether the call holds the entry in full: served
// earlier in the same call or carried whole by a piece served in it, or sent
// by the deferral the call follows. A right answer never holds an entry
// (20261121-230157-d-tac-ym8 leaves entries to a decision of their own).
func (w *WorkflowSession) HoldsEntry(id string) bool {
	return w.session.HoldsEntry(w.entryPiece(id))
}

// ServeEntry records an entry's rendered body served in full.
func (w *WorkflowSession) ServeEntry(id, body string) {
	w.session.ServeEntry(w.entryPiece(id), jsonEscaped(body))
}

// jsonEscaped is text as a result's JSON carries it, where a delivered result
// is searched for it. Marshaling a string cannot fail.
func jsonEscaped(text string) string {
	raw, _ := json.Marshal(text)
	return string(raw[1 : len(raw)-1])
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
		return w.HoldsEntry(id)
	}
	return ctx.Reads[id] == engine.ReadFull
}

// repeats is the no-repeat rule over the session's served memory while
// CutRepeats is on; nil otherwise, which serves everything in full.
func (w *WorkflowSession) repeats() *presenters.Repeats {
	if !w.cutRepeats {
		return nil
	}
	return &presenters.Repeats{Held: w.HoldsEntry, Served: w.ServeEntry}
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

// neutral is text as held texts compare across linked sessions, without the
// session's handle.
func (w *WorkflowSession) neutral(text string) string {
	return model.SessionNeutral(text, string(w.ID()))
}

func contentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
