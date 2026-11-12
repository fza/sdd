package engine

import (
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// Piece is one unit of the served memory (20261112-075604-d-tac-t4r): a
// lane, a framing block, a report schema, an entry served in full. Name is
// what an agent names in reread, an entry's ID for an entry; Hash keys the
// content and stays empty for an entry, whose body never changes.
type Piece struct {
	Name string
	Hash string
}

// Served is how the call in flight serves a piece.
type Served struct {
	// Held reports that the call proved it holds the piece, or received it in
	// full earlier in the same call: the caller serves its pointer, under Tag.
	Held bool
	Tag  string
	// Anchor is the fresh anchor to render into the piece served in full; its
	// Tag is empty when the text took none.
	Anchor model.Anchor
}

// AnchorPlacer places tag in a piece's content, drawing the position from
// rnd; false when the content holds no word to anchor.
type AnchorPlacer func(tag string, rnd model.RandomSource) (model.Anchor, bool)

type issuedAnchor struct {
	piece  Piece
	word   string
	covers []string
}

// pieceCall is what one tool call carries and has served so far.
type pieceCall struct {
	held    []model.Proof
	reread  map[string]bool
	full    bool
	issued  map[Piece]string
	covered map[string]string
	pending []pendingAnchor
}

// pendingAnchor is a piece the call served in full, not yet recorded.
type pendingAnchor struct {
	piece  Piece
	tag    string
	word   string
	covers []string
}

// BeginCall starts a call carrying the proofs an agent lists for pieces it
// still holds and the tags or piece names it lists in reread, which serve in
// full.
func (s *Session) BeginCall(held []model.Proof, reread []string) {
	s.call = pieceCall{held: held, reread: map[string]bool{}}
	for _, r := range reread {
		s.call.reread[strings.ToLower(strings.Trim(strings.TrimSpace(r), "⟦⟧"))] = true
	}
}

// Holds reports whether the call in flight holds the piece, with the tag it
// holds it under: received earlier in this call, or proved by tag and word.
// An entry also counts as held when the proved piece carried it whole.
func (s *Session) Holds(p Piece) (string, bool) {
	if tag, ok := s.call.issued[p]; ok {
		return tag, true
	}
	if p.Hash == "" {
		if tag, ok := s.call.covered[p.Name]; ok {
			return tag, true
		}
	}
	if s.call.full || s.call.reread[strings.ToLower(p.Name)] {
		return "", false
	}
	for _, proof := range s.call.held {
		if s.call.reread[proof.Tag] {
			continue
		}
		issued, ok := s.anchors[proof.Tag]
		if !ok || !model.ProofWordMatches(issued.word, proof.Word) {
			continue
		}
		if issued.piece == p || (p.Hash == "" && slices.Contains(issued.covers, p.Name)) {
			return proof.Tag, true
		}
	}
	return "", false
}

// ServePiece decides how the call in flight serves p: its pointer when the
// call holds it, otherwise in full under a fresh tag place puts into the
// content. covers names the entries the piece carries whole, held from then
// on under the same tag. The issued anchor is recorded by RecordAnchors.
func (s *Session) ServePiece(p Piece, covers []string, place AnchorPlacer) Served {
	if tag, ok := s.Holds(p); ok {
		return Served{Held: true, Tag: tag}
	}
	tag := s.freshTag()
	anchor, ok := place(tag, s.random())
	if !ok {
		tag, anchor = "", model.Anchor{}
	}
	if s.call.issued == nil {
		s.call.issued, s.call.covered = map[Piece]string{}, map[string]string{}
	}
	s.call.issued[p] = tag
	for _, id := range covers {
		if _, ok := s.call.covered[id]; !ok {
			s.call.covered[id] = tag
		}
	}
	s.call.pending = append(s.call.pending, pendingAnchor{piece: p, tag: tag, word: anchor.Word, covers: slices.Clone(covers)})
	return Served{Anchor: anchor}
}

// PieceMark marks the pieces the call has served in full so far.
func (s *Session) PieceMark() int { return len(s.call.pending) }

// DropPiecesSince withdraws the pieces served in full after mark, for text
// cut before it reached the agent.
func (s *Session) DropPiecesSince(mark int) {
	if mark < 0 || mark > len(s.call.pending) {
		return
	}
	for _, dropped := range s.call.pending[mark:] {
		delete(s.call.issued, dropped.piece)
		for _, id := range dropped.covers {
			if s.call.covered[id] == dropped.tag {
				delete(s.call.covered, id)
			}
		}
	}
	s.call.pending = s.call.pending[:mark]
}

// RecordAnchors logs the anchors the call issued since the last record, so a
// restarted server still checks their proofs.
func (s *Session) RecordAnchors() {
	var records []any
	for _, p := range s.call.pending {
		if p.tag == "" {
			continue
		}
		record := map[string]any{"tag": p.tag, "name": p.piece.Name, "word": p.word}
		if p.piece.Hash != "" {
			record["hash"] = p.piece.Hash
		}
		if len(p.covers) > 0 {
			record["covers"] = p.covers
		}
		s.foldAnchor(record)
		records = append(records, record)
	}
	s.call.pending = nil
	if len(records) == 0 {
		return
	}
	s.appendEvent("", EventAnchors, map[string]any{"anchors": records})
}

func (s *Session) foldAnchor(record any) {
	fields, _ := record.(map[string]any)
	tag, _ := fields["tag"].(string)
	if tag == "" {
		return
	}
	name, _ := fields["name"].(string)
	hash, _ := fields["hash"].(string)
	word, _ := fields["word"].(string)
	issued := issuedAnchor{piece: Piece{Name: name, Hash: hash}, word: word}
	switch covers := fields["covers"].(type) {
	case []string:
		issued.covers = covers
	case []any:
		for _, v := range covers {
			if id, ok := v.(string); ok {
				issued.covers = append(issued.covers, id)
			}
		}
	}
	if s.anchors == nil {
		s.anchors = map[string]issuedAnchor{}
	}
	s.anchors[tag] = issued
}

func (s *Session) freshTag() string {
	for {
		tag := model.NewAnchorTag(s.random())
		if _, taken := s.anchors[tag]; taken {
			continue
		}
		if slices.ContainsFunc(s.call.pending, func(p pendingAnchor) bool { return p.tag == tag }) {
			continue
		}
		return tag
	}
}

func (s *Session) random() model.RandomSource {
	if s.rnd == nil {
		s.rnd = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	return s.rnd
}
