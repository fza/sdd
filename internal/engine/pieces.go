package engine

import (
	"maps"
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

// HoldRank is how likely the next serve repeats a piece a serve carried, as
// the hold-next line orders it (20261114-121534-d-tac-qdb).
type HoldRank int

const (
	// HoldStep is an instruction lane or the report schema of a step.
	HoldStep HoldRank = iota
	// HoldCarried is any other piece: a framing block, the vocabulary, an
	// entry.
	HoldCarried
)

// MaxHoldNext caps the hold-next line.
const MaxHoldNext = 20

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
	// followed is what the deferral this call follows sent, still pointed at
	// should the call leave lanes waiting again.
	followed map[Piece]string
	// serving is the instance whose serve the call composes (ServeFor), served
	// every instance it composed; carried is what it served outside them.
	serving string
	served  map[string]bool
	carried []Piece
}

// pendingAnchor is a piece the call served, not yet recorded: in full, or as
// its pointer when held; changed marks one that serves once per change of
// its content.
type pendingAnchor struct {
	piece    Piece
	tag      string
	word     string
	covers   []string
	changed  bool
	held     bool
	rank     HoldRank
	instance string
}

// stepServe is what an instance's last serve carried, numbered by stepSeq.
type stepServe struct {
	seq    int
	pieces []rankedPiece
}

type rankedPiece struct {
	piece Piece
	rank  HoldRank
}

// HeldPiece is a piece the hold-next line names, by the tag it was last
// served under.
type HeldPiece struct {
	Name string
	Tag  string
}

// HoldHints names the entries only the caller knows the next serve may
// repeat, in the order the hold-next line takes them.
type HoldHints struct {
	// Knowledge is the type-system overview and the kind guides while a
	// capture runs.
	Knowledge []string
	// Reading is the entries the open readings depend on.
	Reading []string
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

// ServeFor makes the pieces the call serves from here on belong to the serve
// of instance, until the next ServeFor.
func (s *Session) ServeFor(instance string) {
	s.call.serving = instance
	if s.call.served == nil {
		s.call.served = map[string]bool{}
	}
	s.call.served[instance] = true
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
// on under the same tag. rank places p on the hold-next lines that follow.
// The issued anchor is recorded by RecordAnchors.
func (s *Session) ServePiece(p Piece, rank HoldRank, covers []string, place AnchorPlacer) Served {
	if tag, ok := s.Holds(p); ok {
		s.call.pending = append(s.call.pending, pendingAnchor{piece: p, tag: tag, held: true, rank: rank, instance: s.call.serving})
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
	s.call.pending = append(s.call.pending, pendingAnchor{piece: p, tag: tag, word: anchor.Word, covers: slices.Clone(covers), rank: rank, instance: s.call.serving})
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
			s.call.pending = append(s.call.pending, pendingAnchor{piece: p, changed: true})
		}
		return false
	}
	if last == p.Hash && !s.call.full {
		return false
	}
	s.call.pending = append(s.call.pending, pendingAnchor{piece: p, changed: true})
	return true
}

// PieceMark marks the pieces the call has served so far.
func (s *Session) PieceMark() int { return len(s.call.pending) }

// DropPiecesSince withdraws the pieces served after mark, for text cut
// before it reached the agent.
func (s *Session) DropPiecesSince(mark int) {
	if mark < 0 || mark > len(s.call.pending) {
		return
	}
	for _, dropped := range s.call.pending[mark:] {
		if dropped.held {
			continue
		}
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
// restarted server still checks their proofs, with what each instance's
// serve carried and the pieces served in full where a proof could have held
// them, which the hold-next lines of their move name from then on.
func (s *Session) RecordAnchors() {
	var records []any
	steps := map[string]any{}
	for instance := range s.call.served {
		steps[instance] = []any{}
	}
	for _, p := range s.call.pending {
		if p.tag != "" {
			s.noteCarried(steps, p)
		}
		if p.held || p.tag == "" && !p.changed {
			continue
		}
		record := map[string]any{"name": p.piece.Name}
		if p.tag != "" {
			record["tag"], record["word"] = p.tag, p.word
			if s.couldHavePointed(p.piece) {
				record["repeat"] = s.moveOf(p.instance)
			}
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
		s.foldAnchor(record)
		records = append(records, record)
	}
	s.call.pending, s.call.served = nil, nil
	for instance, pieces := range steps {
		if served, ok := s.steps[instance]; ok && slices.Equal(served.pieces, rankedPieces(pieces)) {
			delete(steps, instance)
		}
	}
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

// noteCarried adds p to what its instance's serve carried, or to what the
// call served outside a serve.
func (s *Session) noteCarried(steps map[string]any, p pendingAnchor) {
	if p.instance == "" {
		if !slices.Contains(s.call.carried, p.piece) {
			s.call.carried = append(s.call.carried, p.piece)
		}
		return
	}
	pieces, _ := steps[p.instance].([]any)
	for _, noted := range pieces {
		if fields := noted.(map[string]any); fields["name"] == p.piece.Name && fields["hash"] == p.piece.Hash {
			return
		}
	}
	steps[p.instance] = append(pieces, map[string]any{"name": p.piece.Name, "hash": p.piece.Hash, "rank": int(p.rank)})
}

// couldHavePointed reports whether an earlier call issued p, so a proof
// would have held it, and the call in flight asked for it neither by reread
// nor by a reorientation.
func (s *Session) couldHavePointed(p Piece) bool {
	tag, issued := s.latest[p]
	return issued && !s.call.full && !s.call.reread[strings.ToLower(p.Name)] && !s.call.reread[tag]
}

// moveOf is the move a piece served for instance belongs to: the instance,
// or for a piece served outside any serve the newest running move; empty
// when none runs.
func (s *Session) moveOf(instance string) string {
	if instance != "" {
		return instance
	}
	for _, id := range slices.Backward(s.order) {
		if s.instances[id].Status == StatusRunning {
			return id
		}
	}
	return ""
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
	if s.anchors == nil {
		s.anchors = map[string]issuedAnchor{}
	}
	if s.latest == nil {
		s.latest = map[Piece]string{}
	}
	s.anchors[tag] = issued
	s.latest[issued.piece] = tag
	if move, ok := fields["repeat"].(string); ok && !slices.Contains(s.repeats[move], issued.piece) {
		if s.repeats == nil {
			s.repeats = map[string][]Piece{}
		}
		s.repeats[move] = append(s.repeats[move], issued.piece)
	}
}

// foldSteps replaces what each instance's last serve carried.
func (s *Session) foldSteps(steps map[string]any) {
	for _, instance := range slices.Sorted(maps.Keys(steps)) {
		if s.steps == nil {
			s.steps = map[string]stepServe{}
		}
		s.stepSeq++
		s.steps[instance] = stepServe{seq: s.stepSeq, pieces: rankedPieces(steps[instance])}
	}
}

// rankedPieces reads the pieces a step record lists, live or replayed.
func rankedPieces(records any) []rankedPiece {
	list, _ := records.([]any)
	pieces := make([]rankedPiece, 0, len(list))
	for _, record := range list {
		fields, _ := record.(map[string]any)
		name, _ := fields["name"].(string)
		hash, _ := fields["hash"].(string)
		var rank HoldRank
		switch r := fields["rank"].(type) {
		case int:
			rank = HoldRank(r)
		case float64:
			rank = HoldRank(r)
		}
		pieces = append(pieces, rankedPiece{piece: Piece{Name: name, Hash: hash}, rank: rank})
	}
	return pieces
}

// HoldNext names the pieces the next serve may repeat by the tags they were
// last served under, at most MaxHoldNext, likeliest first
// (20261114-121534-d-tac-qdb): the lanes and report schemas of the steps the
// running moves stand at, newest serve first; the hints' knowledge, then
// reading entries; the pieces the running moves served in full where a proof
// could have held them; then the rest those steps carried and what the call
// served outside them. A piece without a tag, such as a gather brief line,
// is never named.
func (s *Session) HoldNext(hints HoldHints) []HeldPiece {
	var line []HeldPiece
	named := map[string]bool{}
	add := func(p Piece) {
		tag, ok := s.latest[p]
		if !ok || named[tag] || len(line) == MaxHoldNext {
			return
		}
		named[tag] = true
		line = append(line, HeldPiece{Name: p.Name, Tag: tag})
	}
	serves := s.runningSteps()
	for _, serve := range serves {
		for _, p := range serve.pieces {
			if p.rank == HoldStep {
				add(p.piece)
			}
		}
	}
	for _, id := range slices.Concat(hints.Knowledge, hints.Reading) {
		add(Piece{Name: id})
	}
	for _, move := range s.runningMoves() {
		for _, p := range s.repeats[move] {
			add(p)
		}
	}
	for _, serve := range serves {
		for _, p := range serve.pieces {
			if p.rank != HoldStep {
				add(p.piece)
			}
		}
	}
	for _, p := range s.call.carried {
		add(p)
	}
	return line
}

// runningMoves lists the running instances, newest first, then the session
// itself, under which pieces served while no move ran are kept.
func (s *Session) runningMoves() []string {
	var moves []string
	for _, id := range slices.Backward(s.order) {
		if s.instances[id].Status == StatusRunning {
			moves = append(moves, id)
		}
	}
	return append(moves, "")
}

// runningSteps lists what the last serve of each running instance carried,
// newest serve first.
func (s *Session) runningSteps() []stepServe {
	var serves []stepServe
	for _, move := range s.runningMoves() {
		if serve, ok := s.steps[move]; ok && move != "" {
			serves = append(serves, serve)
		}
	}
	slices.SortFunc(serves, func(a, b stepServe) int { return b.seq - a.seq })
	return serves
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
		if !p.held && p.tag != "" && strings.Contains(delivered, model.AnchorMark(p.tag)) {
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
