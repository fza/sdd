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

// ServedPiece is how a call serves one piece under the anchor proof
// (20261112-075604-d-tac-t4r): when Held, the call proved it holds the piece
// under Tag and gets its pointer; otherwise Text is the piece with its fresh
// Tag placed, Tag empty when the text holds no word to anchor.
type ServedPiece struct {
	Held bool
	Tag  string
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

// ServeText serves a piece of text named name. covers names the entries the
// text carries whole, which its proof holds too.
func (w *WorkflowSession) ServeText(name, text string, covers []string) ServedPiece {
	served := w.session.ServePiece(engine.Piece{Name: name, Hash: contentHash(text)}, covers,
		func(tag string, rnd model.RandomSource) (model.Anchor, bool) {
			return model.PlaceAnchor(text, tag, rnd)
		})
	if served.Held {
		return ServedPiece{Held: true, Tag: served.Tag}
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
		return ServedPiece{Held: true, Tag: served.Tag}, nil, nil
	}
	return ServedPiece{Tag: served.Anchor.Tag}, served.Anchor.RenderSchema(schema), nil
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

// HoldsEntry reports whether the call holds the entry in full, with the tag
// it holds it under: proved by the entry's own tag or by a piece that
// carried it whole.
func (w *WorkflowSession) HoldsEntry(id string) (string, bool) {
	return w.session.Holds(engine.Piece{Name: id})
}

// ServeEntry returns an entry's rendered body served in full, its tag
// placed.
func (w *WorkflowSession) ServeEntry(id, body string) string {
	served := w.session.ServePiece(engine.Piece{Name: id}, nil,
		func(tag string, rnd model.RandomSource) (model.Anchor, bool) {
			return model.PlaceAnchor(body, tag, rnd)
		})
	return served.Anchor.Render(body)
}

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
		_, held := w.HoldsEntry(id)
		return held
	}
	return ctx.Reads[id] == engine.ReadFull
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
