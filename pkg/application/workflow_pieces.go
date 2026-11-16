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

// ServedPiece is how a call serves one piece under the anchor proof:
// when Held, the call proved it holds the piece
// under Tag and gets its pointer, Next naming the word whose follower its
// next proof names; otherwise Text is the piece with its fresh
// Tag placed, Tag empty when the text holds no word to anchor.
type ServedPiece struct {
	Held bool
	Tag  string
	Next string
	Text string
}

// BeginCall opens a tool call carrying the proofs the agent lists in held,
// each "tag:word", and the tags or piece names it lists in reread.
func (w *WorkflowSession) BeginCall(held, reread []string) error {
	proofs := make([]model.Proof, 0, len(held))
	for _, h := range held {
		proof, err := model.ParseProof(h)
		if err != nil {
			return fmt.Errorf("held: %w", err)
		}
		proofs = append(proofs, proof)
	}
	w.session.BeginCall(proofs, reread)
	return nil
}

// FailedProof is a proof the call listed in held that holds no piece, as
// "tag:word"; Issued reports whether the tag was ever issued, so only the
// word is wrong.
type FailedProof struct {
	Proof  string
	Issued bool
}

// FailedProofs lists the proofs of the call that hold no piece: a tag never
// issued or a word its tag never asked for.
func (w *WorkflowSession) FailedProofs() []FailedProof {
	var failed []FailedProof
	for _, f := range w.session.FailedProofs() {
		failed = append(failed, FailedProof{Proof: f.Proof.String(), Issued: f.Issued})
	}
	return failed
}

// ServeText serves a piece of text named name. covers names the entries the
// text carries whole, which its proof holds too.
func (w *WorkflowSession) ServeText(name, text string, covers []string) ServedPiece {
	return w.ServeTextIn(name, text, text, covers)
}

// ServeTextIn is ServeText for a piece joined with others into field, the
// text one result field carries: its proofs ask for words that occur once
// in field (model.PlaceAnchorIn).
func (w *WorkflowSession) ServeTextIn(name, text, field string, covers []string) ServedPiece {
	// The pointers of held entries a lane carries rotate their next proof;
	// the lane stays the same piece.
	served := w.session.ServePiece(engine.Piece{Name: name, Hash: contentHash(model.WithoutProofs(text))}, w.entryPieces(covers),
		func(tag string, rnd model.RandomSource) (model.Anchor, bool) {
			return model.PlaceAnchorIn(text, field, tag, rnd)
		})
	if served.Held {
		return ServedPiece{Held: true, Tag: served.Tag, Next: served.Next}
	}
	return ServedPiece{Tag: served.Anchor.Tag, Text: served.Anchor.Render(text)}
}

// ServeSchema serves a report schema named name; in full it returns a copy
// carrying the tag in one of its descriptions.
func (w *WorkflowSession) ServeSchema(name string, schema map[string]any) (ServedPiece, map[string]any, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return ServedPiece{}, nil, fmt.Errorf("encoding the %s report schema: %w", name, err)
	}
	served := w.session.ServePiece(engine.Piece{Name: name, Hash: contentHash(string(raw))}, nil,
		func(tag string, rnd model.RandomSource) (model.Anchor, bool) {
			return model.PlaceSchemaAnchor(schema, tag, rnd)
		})
	if served.Held {
		return ServedPiece{Held: true, Tag: served.Tag, Next: served.Next}, nil, nil
	}
	return ServedPiece{Tag: served.Anchor.Tag}, served.Anchor.RenderSchema(schema), nil
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

// EndDeferral ends the instance's deferral, as a serve of it settles it.
func (w *WorkflowSession) EndDeferral(instance string) {
	w.session.EndDeferral(instance)
}

// RecordDeferral records that the call's serve of the instance left parts
// waiting past the result bound, with every piece the call served whose tag
// delivered, the result's text, carries.
func (w *WorkflowSession) RecordDeferral(instance, delivered string) {
	w.session.RecordDeferral(instance, delivered)
}

// HoldsEntry reports whether the call holds the entry in full, with the tag
// it holds it under: proved by the entry's own tag or by a piece that
// carried it whole. hash is the entry's model.EntryHash. A held entry serves
// as its pointer, so the proof it rests on is spent and next names the word
// whose follower the next proof names.
func (w *WorkflowSession) HoldsEntry(id, hash string) (tag, next string, held bool) {
	served := w.session.Point(engine.Piece{Name: id, Hash: hash})
	return served.Tag, served.Next, served.Held
}

// ServeEntry returns an entry's rendered body served in full, its tag
// placed and its proofs asking for words once in field, the whole result
// text the body is served in; an empty field is the body alone.
func (w *WorkflowSession) ServeEntry(id, hash, body, field string) string {
	if field == "" {
		field = body
	}
	served := w.session.ServePiece(engine.Piece{Name: id, Hash: hash}, nil,
		func(tag string, rnd model.RandomSource) (model.Anchor, bool) {
			return model.PlaceAnchorIn(body, field, tag, rnd)
		})
	return served.Anchor.Render(body)
}

// UnprovedPiece is a piece an earlier call served in full under Tag that no
// call proved since.
type UnprovedPiece struct {
	Name string
	Tag  string
}

// Unproved lists the pieces earlier calls served in full that no call proved
// since, newest first, capped. A finished session lists none.
func (w *WorkflowSession) Unproved() []UnprovedPiece {
	if w.Finished() {
		return nil
	}
	listed := w.session.Unproved()
	pieces := make([]UnprovedPiece, 0, len(listed))
	for _, piece := range listed {
		pieces = append(pieces, UnprovedPiece{Name: piece.Name, Tag: piece.Tag})
	}
	return pieces
}

// DropProved keeps what the call's proofs proved out of the record, for a
// call that failed.
func (w *WorkflowSession) DropProved() { w.session.DropProved() }

// RecordServed logs the anchors the call issued. A finished session records
// nothing: its last serve is the one that ended it.
func (w *WorkflowSession) RecordServed(ctx context.Context, identity RequestIdentity) error {
	if w.Finished() {
		return nil
	}
	w.setOperation(ctx, identity)
	w.session.RecordAnchors()
	return w.session.SinkErr()
}

// servedInFull reports whether the consumer holds the entry in full: by its
// proof while CutRepeats is on, by the read ledger otherwise.
func (w *WorkflowSession) servedInFull(ctx *engine.Context, id string) bool {
	if w.cutRepeats {
		_, held := w.session.Holds(w.entryPiece(id))
		return held
	}
	return ctx.Reads[id] == engine.ReadFull
}

// entryPiece is the piece an entry of the session's graph serves as, keyed
// by its stored text. An entry the graph does not hold gets no hash and so
// matches no proof: it serves in full.
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
	return &presenters.Repeats{Held: w.HoldsEntry, Anchor: w.ServeEntry}
}

func contentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// stripAnchorTags takes the anchor tags out of every string in a value an
// agent reports: text it copied from a served piece carries them.
func stripAnchorTags(value any) any {
	switch v := value.(type) {
	case string:
		return model.StripAnchorTags(v)
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = stripAnchorTags(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = stripAnchorTags(item)
		}
		return out
	case []string:
		out := make([]string, len(v))
		for i, item := range v {
			out[i] = model.StripAnchorTags(item)
		}
		return out
	}
	return value
}
