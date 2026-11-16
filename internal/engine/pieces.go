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
// content, an entry's model.EntryHash, so a piece edited in place since it
// was served serves in full again.
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
	// proof of Tag names; empty when the pointer rests on no proof, as the
	// call's proof still holds, or when the piece serves in full next time.
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
	// followed is what the deferral this call follows sent, still pointed at
	// should the call leave lanes waiting again.
	followed map[Piece]string
	// proven are the tags the call's proofs answer as the call began, with
	// their proof step then, so a proof the call spends still holds for the
	// rest of it and is spent once; spent are those a pointer of the call
	// rested on.
	proven map[string]int
	spent  []spentProof
	// seq orders the pieces and pointers the call serves, so a mark
	// withdraws both.
	seq int
}

// pendingAnchor is a piece the call served in full, not yet recorded;
// changed marks one that serves once per change of its content.
type pendingAnchor struct {
	piece      Piece
	tag        string
	word       string
	covers     []string
	challenges []model.Challenge
	changed    bool
	seq        int
}

// spentProof is the proof under tag a pointer of the call rested on.
type spentProof struct {
	tag string
	seq int
}

func (s *Session) nextSeq() int {
	s.call.seq++
	return s.call.seq - 1
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

// Point is Holds for a pointer the call serves: the proof it rests on is
// spent once the call records, and Next names the word whose follower the
// following proof names.
func (s *Session) Point(p Piece) Served {
	tag, proved, ok := s.holds(p)
	if !ok {
		return Served{}
	}
	if !proved {
		return Served{Held: true, Tag: tag}
	}
	s.call.spent = append(s.call.spent, spentProof{tag: tag, seq: s.nextSeq()})
	return Served{Held: true, Tag: tag, Next: s.anchors[tag].promptAfter(s.call.proven[tag])}
}

// holds is Holds reporting too whether a proof of the call holds p.
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
// content. carried are the entries the piece carries whole, held from then
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
	s.call.pending = append(s.call.pending, pendingAnchor{piece: p, tag: tag, word: anchor.Word, covers: covers, challenges: anchor.Challenges, seq: s.nextSeq()})
	return Served{Anchor: anchor}
}

// ServeChanged reports whether the call serves p, a piece that serves once per
// change of its content: while its content differs from the last the session
// served under p.Name, or when the call serves every piece in full. An empty
// Hash withdraws the content, so the next one serves again; it serves
// nothing. The change is recorded by RecordAnchors.
func (s *Session) ServeChanged(p Piece) bool {
	last := s.changed[p.Name]
	if p.Hash == "" {
		if last != "" {
			s.call.pending = append(s.call.pending, pendingAnchor{piece: p, changed: true, seq: s.nextSeq()})
		}
		return false
	}
	if last == p.Hash && !s.call.full {
		return false
	}
	s.call.pending = append(s.call.pending, pendingAnchor{piece: p, changed: true, seq: s.nextSeq()})
	return true
}

// PieceMark marks the pieces and pointers the call has served so far.
func (s *Session) PieceMark() int { return s.call.seq }

// DropPiecesSince withdraws the pieces served in full after mark, and the
// proofs pointers served after it rested on, for text cut before it reached
// the agent.
func (s *Session) DropPiecesSince(mark int) {
	if mark < 0 || mark > s.call.seq {
		return
	}
	cut := s.pendingSince(mark)
	for _, dropped := range s.call.pending[cut:] {
		s.unissue(dropped)
	}
	s.call.pending = s.call.pending[:cut]
	s.call.spent = slices.DeleteFunc(s.call.spent, func(p spentProof) bool { return p.seq >= mark })
}

// KeepDelivered withdraws the pieces served in full after mark whose placed
// tag delivered, the text a result carries of them, does not hold, and the
// proofs of pointers served after it whose tag it does not hold: text
// composed into a part the result left out or left waiting. Empty delivered
// withdraws every one, as DropPiecesSince does.
func (s *Session) KeepDelivered(mark int, delivered string) {
	if delivered == "" {
		s.DropPiecesSince(mark)
		return
	}
	if mark < 0 || mark > s.call.seq {
		return
	}
	cut := s.pendingSince(mark)
	kept := s.call.pending[:cut]
	for _, p := range s.call.pending[cut:] {
		if p.changed || (p.tag != "" && strings.Contains(delivered, model.AnchorMark(p.tag)+" ")) {
			kept = append(kept, p)
			continue
		}
		s.unissue(p)
	}
	s.call.pending = kept
	s.call.spent = slices.DeleteFunc(s.call.spent, func(p spentProof) bool {
		return p.seq >= mark && !strings.Contains(delivered, model.AnchorMark(p.tag))
	})
}

// pendingSince is the index of the first pending piece served at or after
// mark.
func (s *Session) pendingSince(mark int) int {
	i, _ := slices.BinarySearchFunc(s.call.pending, mark, func(p pendingAnchor, mark int) int { return p.seq - mark })
	return i
}

func (s *Session) unissue(p pendingAnchor) {
	delete(s.call.issued, p.piece)
	for _, id := range p.covers {
		if s.call.covered[id] == p.tag {
			delete(s.call.covered, id)
		}
	}
}

// RecordAnchors logs the anchors the call issued since the last record, so a
// restarted server still checks their proofs, and spends the proofs its
// pointers rested on.
func (s *Session) RecordAnchors() {
	var records []any
	for _, p := range s.call.pending {
		if p.tag == "" && !p.changed {
			continue
		}
		record := map[string]any{"name": p.piece.Name}
		if p.tag != "" {
			record["tag"], record["word"] = p.tag, p.word
		}
		if p.piece.Hash != "" {
			record["hash"] = p.piece.Hash
		}
		if p.changed {
			record["changed"] = true
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
	steps := map[string]any{}
	for _, spent := range s.call.spent {
		if step, ok := s.call.proven[spent.tag]; ok && s.anchors[spent.tag].step == step {
			steps[spent.tag] = step + 1
		}
	}
	s.call.spent = nil
	s.foldSteps(steps)
	if len(records) == 0 && len(steps) == 0 {
		return
	}
	data := map[string]any{}
	if len(records) > 0 {
		data["anchors"] = records
	}
	if len(steps) > 0 {
		data["steps"] = steps
	}
	s.appendEvent("", EventAnchors, data)
}

// foldAnchors folds an anchors event: the anchors it issued, then the proofs
// it spent.
func (s *Session) foldAnchors(data map[string]any) {
	anchors, _ := data["anchors"].([]any)
	for _, record := range anchors {
		s.foldAnchor(record)
	}
	steps, _ := data["steps"].(map[string]any)
	s.foldSteps(steps)
}

// foldSteps sets each tag's proof step, the number of its proofs spent.
func (s *Session) foldSteps(steps map[string]any) {
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

func (s *Session) foldAnchor(record any) {
	fields, _ := record.(map[string]any)
	name, _ := fields["name"].(string)
	hash, _ := fields["hash"].(string)
	if fields["changed"] == true {
		if s.changed == nil {
			s.changed = map[string]string{}
		}
		s.changed[name] = hash
	}
	tag, _ := fields["tag"].(string)
	if tag == "" {
		return
	}
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

// FollowDeferral makes the call in flight follow the instance's deferral, a
// serve of its step that left lanes waiting past the result bound: the
// pieces the deferral sent count as held in it, without a proof, as the
// result before carried them, so an agent listing no proofs still reaches the
// waiting lanes, by a plain next and by a report alike. Called before the
// serve composes, so the entries it carries print as pointers too.
func (s *Session) FollowDeferral(instance string) {
	sent, ok := s.deferred[instance]
	if !ok {
		return
	}
	if s.call.issued == nil {
		s.call.issued, s.call.covered = map[Piece]string{}, map[string]string{}
	}
	for p, tag := range sent {
		s.call.issued[p] = tag
	}
	s.call.followed = sent
}

// EndDeferral ends the instance's deferral. Every serve of the instance
// settles it, so the one a call follows is always its last serve's.
func (s *Session) EndDeferral(instance string) {
	if _, ok := s.deferred[instance]; !ok {
		return
	}
	ended := map[string]any{"ended": true}
	s.foldDeferral(instance, ended)
	s.appendEvent(instance, EventDeferred, ended)
}

// RecordDeferral records that the call's serve of the instance left lanes
// waiting: every piece the call served in full whose tag the delivered
// result carries, with those of a deferral it follows, serves as a pointer
// in the call that follows. An entry composed into a lane that waits carries
// no tag in the result, so it serves in full with that lane.
func (s *Session) RecordDeferral(instance, delivered string) {
	pieces := maps.Clone(s.call.followed)
	if pieces == nil {
		pieces = map[Piece]string{}
	}
	for _, p := range s.call.pending {
		if p.tag != "" && strings.Contains(delivered, model.AnchorMark(p.tag)) {
			pieces[p.piece] = p.tag
		}
	}
	records := make([]any, 0, len(pieces))
	for p, tag := range pieces {
		records = append(records, map[string]any{"name": p.Name, "hash": p.Hash, "tag": tag})
	}
	data := map[string]any{"sent": records}
	s.foldDeferral(instance, data)
	s.appendEvent(instance, EventDeferred, data)
}

func (s *Session) foldDeferral(instance string, data map[string]any) {
	if data["ended"] == true {
		delete(s.deferred, instance)
		return
	}
	pieces := map[Piece]string{}
	sent, _ := data["sent"].([]any)
	for _, record := range sent {
		fields, _ := record.(map[string]any)
		name, _ := fields["name"].(string)
		hash, _ := fields["hash"].(string)
		tag, _ := fields["tag"].(string)
		pieces[Piece{Name: name, Hash: hash}] = tag
	}
	if s.deferred == nil {
		s.deferred = map[string]map[Piece]string{}
	}
	s.deferred[instance] = pieces
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
