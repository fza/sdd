package engine

import (
	"maps"
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// Piece is one unit of the served memory: a
// lane, a framing block, a report schema, an entry served in full. Name is
// what an agent names in reread, an entry's ID for an entry; Hash keys the
// content, so a piece edited in place since it was served serves in full
// again.
type Piece struct {
	Name string
	Hash string
}

// key is how a piece another piece carries whole is named in its covers.
func (p Piece) key() string { return p.Name + "@" + p.Hash }

// Served is how the call in flight serves a piece.
type Served struct {
	// Held reports that the call proved it holds the piece, or received it in
	// full earlier in the same call: the caller serves its pointer, under Tag.
	Held bool
	Tag  string
	// Next is the word, once in the piece's text, whose follower the next
	// proof of Tag names; empty when the pointer needs none or the piece
	// serves in full the next time.
	Next string
	// Anchor is the fresh anchor to render into the piece served in full; its
	// Tag is empty when the text took none.
	Anchor model.Anchor
}

// AnchorPlacer places tag in a piece's content, drawing the position from
// rnd; false when the content holds no word to anchor.
type AnchorPlacer func(tag string, rnd model.RandomSource) (model.Anchor, bool)

// issuedAnchor is a tag served with a piece: proof number step names the
// word after the tag, each later one a challenge's answer, so a proof
// replayed from an earlier call never holds.
type issuedAnchor struct {
	piece      Piece
	word       string
	covers     []string
	challenges []model.Challenge
	step       int
}

// answer is the word the anchor's next proof names; false once its
// challenges are spent.
func (a issuedAnchor) answer() (string, bool) {
	if a.step == 0 {
		return a.word, true
	}
	if a.step-1 < len(a.challenges) {
		return a.challenges[a.step-1].Answer, true
	}
	return "", false
}

// proves reports whether word answers the anchor's next proof.
func (a issuedAnchor) proves(word string) bool {
	answer, ok := a.answer()
	return ok && model.ProofWordMatches(answer, word)
}

// promptAfter is the prompt the proof after proof number step answers.
func (a issuedAnchor) promptAfter(step int) string {
	if step < len(a.challenges) {
		return a.challenges[step].Prompt
	}
	return ""
}

// pieceCall is what one tool call carries and has served so far.
type pieceCall struct {
	held    []model.Proof
	reread  map[string]bool
	full    bool
	issued  map[Piece]string
	covered map[string]string
	pending []pendingAnchor
	// proven are the tags the call's proofs answer as the call began, with
	// their proof step then, so a proof the call spends still holds for the
	// rest of it and is spent once; spent are those a pointer of the call
	// rested on, each with the step of the call it was spent at.
	proven map[string]int
	spent  map[string]int
	// steps counts what the call served in full or spent, for PieceMark.
	steps int
}

// pendingAnchor is a piece the call served in full, not yet recorded.
type pendingAnchor struct {
	step       int
	piece      Piece
	tag        string
	word       string
	covers     []string
	challenges []model.Challenge
}

// BeginCall starts a call carrying the proofs an agent lists for pieces it
// still holds and the tags or piece names it lists in reread, which serve in
// full.
func (s *Session) BeginCall(held []model.Proof, reread []string) {
	s.call = pieceCall{held: held, reread: map[string]bool{}, proven: map[string]int{}}
	for _, r := range reread {
		s.call.reread[strings.ToLower(strings.Trim(strings.TrimSpace(r), "⟦⟧"))] = true
	}
	for _, proof := range held {
		if issued, ok := s.anchors[proof.Tag]; ok && issued.proves(proof.Word) {
			s.call.proven[proof.Tag] = issued.step
		}
	}
}

// Holds reports whether the call in flight holds the piece, with the tag it
// holds it under: received earlier in this call, or proved by tag and word.
// An entry also counts as held when the proved piece carried it whole.
func (s *Session) Holds(p Piece) (string, bool) {
	tag, _, ok := s.holds(p)
	return tag, ok
}

// Point is Holds for a pointer the call serves: a proof it rests on is spent,
// and Next names the word the following proof answers.
func (s *Session) Point(p Piece) Served {
	tag, proved, ok := s.holds(p)
	if !ok {
		return Served{}
	}
	if !proved {
		return Served{Held: true, Tag: tag}
	}
	if s.call.spent == nil {
		s.call.spent = map[string]int{}
	}
	if _, spent := s.call.spent[tag]; !spent {
		s.call.spent[tag] = s.call.steps
		s.call.steps++
	}
	return Served{Held: true, Tag: tag, Next: s.anchors[tag].promptAfter(s.call.proven[tag])}
}

func (s *Session) holds(p Piece) (string, bool, bool) {
	if tag, ok := s.call.issued[p]; ok {
		return tag, false, true
	}
	if tag, ok := s.call.covered[p.key()]; ok {
		return tag, false, true
	}
	if s.call.full || s.call.reread[strings.ToLower(p.Name)] {
		return "", false, false
	}
	for _, proof := range s.call.held {
		if s.call.reread[proof.Tag] {
			continue
		}
		if _, proven := s.call.proven[proof.Tag]; !proven {
			continue
		}
		issued := s.anchors[proof.Tag]
		if issued.piece == p || slices.Contains(issued.covers, p.key()) {
			return proof.Tag, true, true
		}
	}
	return "", false, false
}

// ServePiece decides how the call in flight serves p: its pointer when the
// call holds it, otherwise in full under a fresh tag place puts into the
// content. covers are the entries the piece carries whole, held from then
// on under the same tag. The issued anchor is recorded by RecordAnchors.
func (s *Session) ServePiece(p Piece, carried []Piece, place AnchorPlacer) Served {
	covers := make([]string, len(carried))
	for i, c := range carried {
		covers[i] = c.key()
	}
	if served := s.Point(p); served.Held {
		return served
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
	s.call.pending = append(s.call.pending, pendingAnchor{step: s.call.steps, piece: p, tag: tag, word: anchor.Word, covers: covers, challenges: anchor.Challenges})
	s.call.steps++
	return Served{Anchor: anchor}
}

// PieceMark marks what the call has served in full or spent so far.
func (s *Session) PieceMark() int { return s.call.steps }

// DropPiecesSince withdraws the pieces served in full after mark, for text
// cut before it reached the agent, and leaves the proofs the withdrawn
// pointers rested on unspent.
func (s *Session) DropPiecesSince(mark int) {
	kept := s.call.pending[:0]
	for _, p := range s.call.pending {
		if p.step < mark {
			kept = append(kept, p)
			continue
		}
		delete(s.call.issued, p.piece)
		for _, id := range p.covers {
			if s.call.covered[id] == p.tag {
				delete(s.call.covered, id)
			}
		}
	}
	s.call.pending = kept
	for tag, step := range s.call.spent {
		if step >= mark {
			delete(s.call.spent, tag)
		}
	}
}

// RecordAnchors logs the anchors the call issued since the last record, so a
// restarted server still checks their proofs, and spends the proofs its
// pointers rested on.
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
		if len(p.challenges) > 0 {
			challenges := make([]any, len(p.challenges))
			for i, c := range p.challenges {
				challenges[i] = []any{c.Prompt, c.Answer}
			}
			record["challenges"] = challenges
		}
		s.foldAnchor(record)
		records = append(records, record)
	}
	s.call.pending = nil
	if len(records) > 0 {
		s.appendEvent("", EventAnchors, map[string]any{"anchors": records})
	}
	steps := map[string]any{}
	for _, tag := range slices.Sorted(maps.Keys(s.call.spent)) {
		issued, ok := s.anchors[tag]
		if !ok || issued.step != s.call.proven[tag] {
			continue
		}
		issued.step++
		s.anchors[tag] = issued
		steps[tag] = issued.step
	}
	s.call.spent = nil
	if len(steps) == 0 {
		return
	}
	s.appendEvent("", EventAnchorSteps, map[string]any{"steps": steps})
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
	challenges, _ := fields["challenges"].([]any)
	for _, v := range challenges {
		pair, _ := v.([]any)
		if len(pair) != 2 {
			continue
		}
		prompt, _ := pair[0].(string)
		answer, _ := pair[1].(string)
		if prompt != "" && answer != "" {
			issued.challenges = append(issued.challenges, model.Challenge{Prompt: prompt, Answer: answer})
		}
	}
	if s.anchors == nil {
		s.anchors = map[string]issuedAnchor{}
	}
	s.anchors[tag] = issued
}

// foldAnchorSteps replays the proofs earlier calls spent.
func (s *Session) foldAnchorSteps(data map[string]any) {
	steps, _ := data["steps"].(map[string]any)
	for tag, v := range steps {
		issued, ok := s.anchors[tag]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case int:
			issued.step = n
		case float64:
			issued.step = int(n)
		}
		s.anchors[tag] = issued
	}
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
