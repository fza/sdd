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
	reread     map[string]bool
	full       bool
	issued     map[Piece]bool
	covered    map[string]bool
	pending    []pendingServe
	// seq orders the pieces the call serves, so a mark withdraws them.
	seq int
}

// pendingServe is a piece the call served in full, not yet recorded: text is
// what a question about it is made from, empty for an entry; changed marks
// one that serves once per change of its content.
type pendingServe struct {
	piece   Piece
	text    string
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
// the texts whose latest copy is at least that question's hold for the call;
// lost, a wrong answer or none holds nothing.
func (s *Session) BeginCall(answer string, reread []string) {
	s.call = pieceCall{reread: map[string]bool{}}
	for _, r := range reread {
		s.call.reread[strings.ToLower(strings.TrimSpace(r))] = true
	}
	if strings.TrimSpace(answer) == "" || model.IsLost(answer) {
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
}

// Answered reports whether the call's answer answers a question still
// unanswered.
func (s *Session) Answered() bool { return s.call.answered > 0 }

// Holds reports whether the call in flight holds the piece: received earlier
// in this call, carried whole by a piece received in it, or held by a right
// answer. A piece the call lists in reread is never held, nor one it
// receives in full again.
func (s *Session) Holds(p Piece) bool { return s.holds(p, true) }

func (s *Session) holds(p Piece, byAnswer bool) bool {
	if s.call.issued[p] || s.call.covered[p.key()] {
		return true
	}
	if s.call.full || s.call.reread[strings.ToLower(p.Name)] {
		return false
	}
	held, ok := s.texts[p]
	return byAnswer && ok && s.call.answered > 0 && held.latest >= s.call.provedFrom
}

// ServePiece decides how the call in flight serves p: true for its pointer
// when the call holds it, false for in full. text is what a later question
// about it is made from. carried are the entries the piece carries whole,
// held for the rest of the call. The serve is recorded by RecordServed.
func (s *Session) ServePiece(p Piece, carried []Piece, text string) bool {
	if s.Holds(p) {
		return true
	}
	s.issue(p, carried, text)
	return false
}

// ServeEntry is ServePiece for an entry: a right answer never holds it, so
// an entry served in an earlier call serves in full again (Holds without the
// answer). It is no question's text.
func (s *Session) ServeEntry(p Piece) bool {
	if s.holds(p, false) {
		return true
	}
	s.issue(p, nil, "")
	return false
}

// HoldsEntry reports whether the call holds an entry, as ServeEntry reads it.
func (s *Session) HoldsEntry(p Piece) bool { return s.holds(p, false) }

func (s *Session) issue(p Piece, carried []Piece, text string) {
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
	s.call.pending = append(s.call.pending, pendingServe{piece: p, text: text, covers: covers, seq: s.nextSeq()})
}

// ServeChanged reports whether the call serves p, a piece that serves once per
// change of its content: while its content differs from the last the session
// served under p.Name, or when the call serves every piece in full. An empty
// Hash withdraws the content, so the next one serves again; it serves
// nothing. The change is recorded by RecordServed.
func (s *Session) ServeChanged(p Piece) bool {
	last := s.changed[p.Name]
	if p.Hash == "" {
		if last != "" {
			s.call.pending = append(s.call.pending, pendingServe{piece: p, changed: true, seq: s.nextSeq()})
		}
		return false
	}
	if last == p.Hash && !s.call.full {
		return false
	}
	s.call.pending = append(s.call.pending, pendingServe{piece: p, changed: true, seq: s.nextSeq()})
	return true
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
// question the call's answer answered.
func (s *Session) RecordServed() {
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
		} else if _, known := s.texts[p.piece]; !known {
			record["text"] = p.text
		}
		s.foldText(record)
		records = append(records, record)
	}
	s.call.pending = nil
	data := map[string]any{}
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
// a call that failed: its question stays open for a later call.
func (s *Session) DropAnswer() { s.call.answered = 0 }

// Ask ends a response with a question: about the text held longest by its
// latest copy, the next oldest where that text yields no question not asked
// before, its phrase occurring in no other held text and its answer no
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

// foldTexts folds a texts event: the texts it served, then the question it
// answered.
func (s *Session) foldTexts(data map[string]any) {
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
		s.changed[name] = hash
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

func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	}
	return 0, false
}

func (s *Session) random() model.RandomSource {
	if s.rnd == nil {
		s.rnd = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	return s.rnd
}
