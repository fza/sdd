package engine

import (
	"maps"
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// Piece is one unit of the served memory: a lane, a framing block, a report
// schema, an entry served in full. Name is what an agent names in reread, an
// entry's ID for an entry; Hash keys the content, an entry's
// model.EntryHash, so a piece edited since it was served is a new text.
type Piece struct {
	Name string
	Hash string
}

// key is how a piece another piece carries whole is named in its covers.
func (p Piece) key() string { return p.Name + "@" + p.Hash }

// heldText is a text the session served in full: what a question about it is
// made from, and its latest copy, the number of its newest serve in full.
type heldText struct {
	text   string
	latest int
}

// askedQuestion is a question a response ended with, about piece as its
// latest copy stood then.
type askedQuestion struct {
	piece    Piece
	question model.TextQuestion
	latest   int
	answered bool
}

// pieceCall is what one tool call carries and has served so far.
type pieceCall struct {
	// answered numbers the question the call's answer answers from 1, 0 for
	// none; held then are the texts whose latest copy is at least provedFrom.
	answered   int
	provedFrom int
	// reset is set by lost or a wrong answer: the held set empties when the
	// call records what it served.
	reset   bool
	reread  map[string]bool
	full    bool
	issued  map[Piece]bool
	covered map[string]bool
	pending []pendingServe
	// followed is what the deferral this call follows sent, by the step that
	// sent it, still pointed at should the call leave lanes waiting again.
	followed map[Piece]string
	// seq orders the pieces the call serves, so a mark withdraws them.
	seq int
	// caller is the mark the call carries (SetCaller).
	caller string
}

// pendingServe is a piece the call served in full, not yet recorded: text is
// what a question about it is made from, empty for an entry; probe is what a
// delivered result holds of it; changed marks one that serves once per
// change of its content.
type pendingServe struct {
	piece   Piece
	text    string
	probe   string
	covers  []string
	changed bool
	seq     int
}

func (s *Session) nextSeq() int {
	s.call.seq++
	return s.call.seq - 1
}

// BeginCall starts a call carrying answer, the agent's answer to a question
// a response asked, and the piece names it lists in reread, which serve in
// full. An answer that names the words of a question still unanswered makes
// the texts whose latest copy is at least that question's hold for the call.
// None holds nothing; lost or a wrong answer holds nothing and empties the
// held set, so only texts served from this call on count as held
// (20261122-105026-d-tac-wl5).
func (s *Session) BeginCall(answer string, reread []string) {
	s.call = pieceCall{reread: map[string]bool{}}
	for _, r := range reread {
		s.call.reread[strings.ToLower(strings.TrimSpace(r))] = true
	}
	if strings.TrimSpace(answer) == "" {
		return
	}
	if model.IsLost(answer) {
		s.call.reset = true
		return
	}
	for i, asked := range s.questions {
		if asked.answered || !asked.question.Answered(answer) {
			continue
		}
		if s.call.answered == 0 || asked.latest < s.call.provedFrom {
			s.call.answered, s.call.provedFrom = i+1, asked.latest
		}
	}
	s.call.reset = s.call.answered == 0
}

// Answered reports whether the call's answer answers a question still
// unanswered.
func (s *Session) Answered() bool { return s.call.answered > 0 }

// Holds reports whether the call in flight holds the piece: received earlier
// in this call, carried whole by a piece received in it, held by a right
// answer, or sent by the deferral the call follows. A piece the call lists in
// reread is never held, nor one it receives in full again.
func (s *Session) Holds(p Piece) bool { return s.holds(p, true) }

func (s *Session) holds(p Piece, byAnswer bool) bool {
	if s.call.issued[p] || s.call.covered[p.key()] {
		return true
	}
	if s.call.full || s.call.reread[strings.ToLower(p.Name)] {
		return false
	}
	if held, ok := s.texts[p]; byAnswer && ok && s.call.answered > 0 && held.latest >= s.call.provedFrom && held.latest >= s.heldFrom {
		return true
	}
	_, sent := s.call.followed[p]
	return sent
}

// ServePiece decides how the call in flight serves p: true for its pointer
// when the call holds it, false for in full. text is what a later question
// about it is made from, probe what a delivered result holds of it. carried
// are the entries the piece carries whole, held for the rest of the call.
// The serve is recorded by RecordServed.
func (s *Session) ServePiece(p Piece, carried []Piece, text, probe string) bool {
	if s.Holds(p) {
		return true
	}
	s.issue(p, carried, text, probe)
	return false
}

// ServeEntry is ServePiece for an entry: a right answer never holds it, so
// an entry served in an earlier call serves in full again (Holds without the
// answer). It is no question's text.
func (s *Session) ServeEntry(p Piece, probe string) bool {
	if s.holds(p, false) {
		return true
	}
	s.issue(p, nil, "", probe)
	return false
}

// HoldsEntry reports whether the call holds an entry, as ServeEntry reads it.
func (s *Session) HoldsEntry(p Piece) bool { return s.holds(p, false) }

func (s *Session) issue(p Piece, carried []Piece, text, probe string) {
	covers := make([]string, len(carried))
	for i, c := range carried {
		covers[i] = c.key()
	}
	if s.call.issued == nil {
		s.call.issued, s.call.covered = map[Piece]bool{}, map[string]bool{}
	}
	s.call.issued[p] = true
	for _, key := range covers {
		s.call.covered[key] = true
	}
	s.call.pending = append(s.call.pending, pendingServe{piece: p, text: text, probe: probe, covers: covers, seq: s.nextSeq()})
}

// ServeChanged reports whether the call serves p, a piece that serves once per
// change of its content: while its content differs from the last the session
// served under p.Name, or when the call serves every piece in full. An empty
// Hash withdraws the content, so the next one serves again; it serves
// nothing. The change is recorded by RecordServed.
func (s *Session) ServeChanged(p Piece) bool {
	if p.Hash == "" {
		if s.changed[changedKey(s.call.caller, p.Name)] != "" {
			s.call.pending = append(s.call.pending, pendingServe{piece: p, changed: true, seq: s.nextSeq()})
		}
		return false
	}
	if !s.Changed(p) {
		return false
	}
	s.call.pending = append(s.call.pending, pendingServe{piece: p, changed: true, seq: s.nextSeq()})
	return true
}

// Changed reports whether ServeChanged would serve p, recording nothing: its
// content differs from the last the session served the call's caller under
// p.Name, or the call serves every piece in full. Content the call served
// already counts as served.
func (s *Session) Changed(p Piece) bool {
	if s.call.full {
		return !slices.ContainsFunc(s.call.pending, func(sent pendingServe) bool { return sent.changed && sent.piece == p })
	}
	last := s.changed[changedKey(s.call.caller, p.Name)]
	for _, sent := range s.call.pending {
		if sent.changed && sent.piece.Name == p.Name {
			last = sent.piece.Hash
		}
	}
	return last != p.Hash
}

// SetCaller makes the call in flight serve the caller it names: what serves
// once per change counts per caller, so a caller the session has not served
// gets every such piece in full. The empty caller is the session's first
// (20261121-233256-d-tac-mm2).
func (s *Session) SetCaller(caller string) { s.call.caller = caller }

// changedKey keys the content a caller was served under a piece's name; the
// first caller's keys are the bare names an older log holds.
func changedKey(caller, name string) string {
	if caller == "" {
		return name
	}
	return caller + "\x00" + name
}

// PieceMark marks the pieces the call has served so far.
func (s *Session) PieceMark() int { return s.call.seq }

// DropPiecesSince withdraws the pieces served in full after mark, for text
// cut before it reached the agent.
func (s *Session) DropPiecesSince(mark int) {
	if mark < 0 || mark > s.call.seq {
		return
	}
	cut := s.pendingSince(mark)
	for _, dropped := range s.call.pending[cut:] {
		s.unissue(dropped)
	}
	s.call.pending = s.call.pending[:cut]
}

// KeepDelivered withdraws the pieces served in full after mark that
// delivered, the text a result carries of them, does not hold: text composed
// into a part the result left out or left waiting. Empty delivered withdraws
// every one, as DropPiecesSince does.
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
		if p.changed || p.delivered(delivered) {
			kept = append(kept, p)
			continue
		}
		s.unissue(p)
	}
	s.call.pending = kept
}

func (p pendingServe) delivered(result string) bool {
	return p.probe != "" && strings.Contains(result, p.probe)
}

// pendingSince is the index of the first pending piece served at or after
// mark.
func (s *Session) pendingSince(mark int) int {
	i, _ := slices.BinarySearchFunc(s.call.pending, mark, func(p pendingServe, mark int) int { return p.seq - mark })
	return i
}

func (s *Session) unissue(p pendingServe) {
	delete(s.call.issued, p.piece)
	for _, key := range p.covers {
		delete(s.call.covered, key)
	}
}

// RecordServed logs what the call served in full since the last record, each
// text with its content the first time the session serves it, so a
// restarted server still makes and checks questions about it, and the
// question the call's answer answered, or that it emptied the held set.
func (s *Session) RecordServed() {
	data := map[string]any{}
	if s.call.reset {
		s.call.reset = false
		s.heldFrom = s.copies + 1
		data["reset"] = true
	}
	var records []any
	for _, p := range s.call.pending {
		if p.text == "" && !p.changed {
			continue
		}
		record := map[string]any{"name": p.piece.Name}
		if p.piece.Hash != "" {
			record["hash"] = p.piece.Hash
		}
		if p.changed {
			record["changed"] = true
			if s.call.caller != "" {
				record["caller"] = s.call.caller
			}
		} else if _, known := s.texts[p.piece]; !known {
			record["text"] = p.text
		}
		s.foldText(record)
		records = append(records, record)
	}
	s.call.pending = nil
	if len(records) > 0 {
		data["texts"] = records
	}
	if n := s.call.answered - 1; n >= 0 && !s.questions[n].answered {
		data["answered"] = n
		s.questions[n].answered = true
	}
	if len(data) == 0 {
		return
	}
	s.appendEvent("", EventTexts, data)
}

// DropAnswer keeps the call's answer from counting a question answered, for
// a call that failed: its question stays open for a later call, and the held
// set stays as it was.
func (s *Session) DropAnswer() { s.call.answered, s.call.reset = 0, false }

// Ask ends a response with a question: about the text held longest by its
// latest copy, the next oldest where that text yields no question not asked
// before, its phrase occurring in no other text served and its answer no
// earlier question's, so an answer replayed from the call history answers
// nothing. It logs the question and returns its line, empty when no held
// text yields one.
func (s *Session) Ask() string {
	pieces := slices.Collect(maps.Keys(s.texts))
	slices.SortFunc(pieces, func(a, b Piece) int {
		if d := s.texts[a].latest - s.texts[b].latest; d != 0 {
			return d
		}
		return strings.Compare(a.key(), b.key())
	})
	lower := make(map[Piece]string, len(pieces))
	for _, p := range pieces {
		lower[p] = strings.ToLower(s.texts[p].text)
	}
	elsewhere := func(own Piece, phrase string) bool {
		phrase = strings.ToLower(phrase)
		for p, text := range lower {
			if p != own && phrase != "" && strings.Contains(text, phrase) {
				return true
			}
		}
		return false
	}
	answeredBefore := func(answer string) bool {
		return slices.ContainsFunc(s.questions, func(asked askedQuestion) bool { return asked.question.Answered(answer) })
	}
	for _, p := range pieces {
		held := s.texts[p]
		if held.latest < s.heldFrom {
			continue
		}
		q, ok := model.DrawQuestion(held.text, s.random(), func(q model.TextQuestion) bool {
			return !s.asked[p.key()+"|"+q.Key()] && !elsewhere(p, q.Phrase) && !elsewhere(p, q.Second) && !answeredBefore(q.Answer)
		})
		if !ok {
			continue
		}
		data := map[string]any{
			"name": p.Name, "hash": p.Hash, "latest": held.latest,
			"kind": string(q.Kind), "phrase": q.Phrase, "answer": q.Answer,
		}
		if q.Second != "" {
			data["second"] = q.Second
		}
		s.foldQuestion(data)
		s.appendEvent("", EventQuestion, data)
		return q.Line(p.Name)
	}
	return ""
}

// foldTexts folds a texts event: the held set it emptied, the texts it
// served, then the question it answered.
func (s *Session) foldTexts(data map[string]any) {
	if data["reset"] == true {
		s.heldFrom = s.copies + 1
	}
	records, _ := data["texts"].([]any)
	for _, record := range records {
		fields, _ := record.(map[string]any)
		s.foldText(fields)
	}
	if n, ok := intValue(data["answered"]); ok && n >= 0 && n < len(s.questions) {
		s.questions[n].answered = true
	}
}

func (s *Session) foldText(fields map[string]any) {
	name, _ := fields["name"].(string)
	hash, _ := fields["hash"].(string)
	if fields["changed"] == true {
		if s.changed == nil {
			s.changed = map[string]string{}
		}
		caller, _ := fields["caller"].(string)
		s.changed[changedKey(caller, name)] = hash
		return
	}
	p := Piece{Name: name, Hash: hash}
	if s.texts == nil {
		s.texts = map[Piece]*heldText{}
	}
	s.copies++
	if held, ok := s.texts[p]; ok {
		held.latest = s.copies
		return
	}
	text, _ := fields["text"].(string)
	if text == "" {
		return
	}
	s.texts[p] = &heldText{text: text, latest: s.copies}
}

func (s *Session) foldQuestion(data map[string]any) {
	str := func(key string) string {
		v, _ := data[key].(string)
		return v
	}
	latest, _ := intValue(data["latest"])
	asked := askedQuestion{
		piece:  Piece{Name: str("name"), Hash: str("hash")},
		latest: latest,
		question: model.TextQuestion{
			Kind: model.QuestionKind(str("kind")), Phrase: str("phrase"), Second: str("second"), Answer: str("answer"),
		},
	}
	s.questions = append(s.questions, asked)
	if s.asked == nil {
		s.asked = map[string]bool{}
	}
	s.asked[asked.piece.key()+"|"+asked.question.Key()] = true
}

// foldLegacyAnchors folds an anchors event a build before the text question
// wrote: only the pieces that serve once per change of their content.
func (s *Session) foldLegacyAnchors(data map[string]any) {
	anchors, _ := data["anchors"].([]any)
	for _, record := range anchors {
		fields, _ := record.(map[string]any)
		if fields["changed"] == true {
			s.foldText(fields)
		}
	}
}

func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	}
	return 0, false
}

// FollowDeferral makes the call in flight follow the instance's deferral, a
// serve of its step that left lanes waiting past the result bound: the
// pieces the deferral sent count as held in it, whatever its answer, as the
// result before carried them, so an agent answering lost still reaches the
// waiting lanes, by a plain next and by a report alike (Holds). Called before
// the serve composes, so the entries it carries print as pointers too.
func (s *Session) FollowDeferral(instance string) {
	s.call.followed = s.deferred[instance]
}

// Resent reports whether the deferral the call follows sent a piece named as
// p at the instance's current step when the call would serve p in full all
// the same: it rereads p, or p's content changed.
func (s *Session) Resent(instance string, p Piece) bool {
	step := s.stepOf(instance)
	if step == "" || s.Holds(p) {
		return false
	}
	for piece, sentAt := range s.call.followed {
		if piece.Name == p.Name && sentAt == step {
			return true
		}
	}
	return false
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
// waiting: every piece the call served in full that delivered, the result,
// holds, with those of a deferral it follows, serves as a pointer in the call
// that follows. A piece served in full replaces one of its name sent before
// at the same step.
func (s *Session) RecordDeferral(instance, delivered string) {
	step := s.stepOf(instance)
	pieces := maps.Clone(s.call.followed)
	if pieces == nil {
		pieces = map[Piece]string{}
	}
	for _, p := range s.call.pending {
		if !p.changed && p.delivered(delivered) {
			maps.DeleteFunc(pieces, func(piece Piece, sentAt string) bool { return piece.Name == p.piece.Name && sentAt == step })
			pieces[p.piece] = step
		}
	}
	records := make([]any, 0, len(pieces))
	for p, sentAt := range pieces {
		records = append(records, map[string]any{"name": p.Name, "hash": p.Hash, "step": sentAt})
	}
	data := map[string]any{"sent": records}
	s.foldDeferral(instance, data)
	s.appendEvent(instance, EventDeferred, data)
}

func (s *Session) stepOf(instance string) string {
	if inst, ok := s.instances[instance]; ok {
		return inst.Step
	}
	return ""
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
		step, _ := fields["step"].(string)
		pieces[Piece{Name: name, Hash: hash}] = step
	}
	if s.deferred == nil {
		s.deferred = map[string]map[Piece]string{}
	}
	s.deferred[instance] = pieces
}

func (s *Session) random() model.RandomSource {
	if s.rnd == nil {
		s.rnd = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	return s.rnd
}
