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
// authored marks a text the agent or the person wrote, which is held but no
// question's text.
type heldText struct {
	text     string
	latest   int
	authored bool
}

// askedQuestion is a question a response ended with, about piece as its
// latest copy stood then. round is the index of the first question of the
// response that asked it: an answer answers a round's questions together.
type askedQuestion struct {
	piece    Piece
	question model.TextQuestion
	latest   int
	round    int
	answered bool
}

// pieceCall is what one tool call carries and has served so far.
type pieceCall struct {
	// answered numbers the round the call's answer answers from 1, 0 for
	// none; held then are the texts whose latest copy is at least provedFrom.
	// short is set once the answer holds a piece the call serves.
	answered   int
	provedFrom int
	short      bool
	// missed is set by lost or a wrong answer, which RecordServed logs.
	missed  bool
	reread  map[string]bool
	full    bool
	issued  map[Piece]bool
	covered map[string]bool
	pending []pendingServe
	// seq orders the pieces the call serves, so a mark withdraws them.
	seq int
}

// pendingServe is a piece the call served in full, not yet recorded: text is
// what a question about it is made from, empty for an entry; changed marks
// one that serves once per change of its content.
type pendingServe struct {
	piece    Piece
	text     string
	covers   []string
	changed  bool
	authored bool
	seq      int
}

func (s *Session) nextSeq() int {
	s.call.seq++
	return s.call.seq - 1
}

// BeginCall starts a call carrying answer, the agent's answers to the
// questions a response asked (model.RoundAnswered), and the piece names it
// lists in reread, which serve in full. An answer that answers every
// question of a round still unanswered makes the texts whose latest copy is
// at least the oldest of that round's hold for the call; lost, a wrong
// answer to any question or none holds nothing.
func (s *Session) BeginCall(answer string, reread []string) {
	s.call = pieceCall{reread: map[string]bool{}}
	for _, r := range reread {
		s.call.reread[strings.ToLower(strings.TrimSpace(r))] = true
	}
	if strings.TrimSpace(answer) == "" {
		return
	}
	if model.IsLost(answer) {
		s.call.missed = true
		return
	}
	for round, asked := range s.openRounds() {
		if !model.RoundAnswered(asked.questions, answer) {
			continue
		}
		earlier := asked.latest < s.call.provedFrom || asked.latest == s.call.provedFrom && round+1 < s.call.answered
		if s.call.answered == 0 || earlier {
			s.call.answered, s.call.provedFrom = round+1, asked.latest
		}
	}
	s.call.missed = s.call.answered == 0
}

// openRound is a round of questions still unanswered: its questions and the
// oldest latest copy they asked about.
type openRound struct {
	questions []model.TextQuestion
	latest    int
}

// openRounds lists the rounds still unanswered by the index of their first
// question.
func (s *Session) openRounds() map[int]*openRound {
	rounds := map[int]*openRound{}
	for _, asked := range s.questions {
		if asked.answered {
			continue
		}
		round, ok := rounds[asked.round]
		if !ok {
			round = &openRound{latest: asked.latest}
			rounds[asked.round] = round
		}
		round.questions = append(round.questions, asked.question)
		round.latest = min(round.latest, asked.latest)
	}
	return rounds
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
	return byAnswer && s.heldByAnswer(p)
}

func (s *Session) heldByAnswer(p Piece) bool {
	held, ok := s.texts[p]
	return ok && s.call.answered > 0 && held.latest >= s.call.provedFrom
}

// ServePiece decides how the call in flight serves p: true for its pointer
// when the call holds it, false for in full. text is what a later question
// about it is made from. carried are the entries the piece carries whole,
// held for the rest of the call. The serve is recorded by RecordServed.
func (s *Session) ServePiece(p Piece, carried []Piece, text string) bool {
	if s.servedShort(p) {
		return true
	}
	s.issue(p, carried, text, false)
	return false
}

// ServeAuthored is ServePiece for text the agent or the person wrote, or
// that embeds their words, such as a draft played back: held as any text,
// it is never a question's text.
func (s *Session) ServeAuthored(p Piece, text string) bool {
	if s.servedShort(p) {
		return true
	}
	s.issue(p, nil, text, true)
	return false
}

// servedShort is Holds, noting a piece the call's answer holds.
func (s *Session) servedShort(p Piece) bool {
	if !s.Holds(p) {
		return false
	}
	if !s.call.issued[p] && !s.call.covered[p.key()] && s.heldByAnswer(p) {
		s.call.short = true
	}
	return true
}

// ServeEntry is ServePiece for an entry: a right answer never holds it, so
// an entry served in an earlier call serves in full again (Holds without the
// answer). It is no question's text.
func (s *Session) ServeEntry(p Piece) bool {
	if s.holds(p, false) {
		return true
	}
	s.issue(p, nil, "", false)
	return false
}

// HoldsEntry reports whether the call holds an entry, as ServeEntry reads it.
func (s *Session) HoldsEntry(p Piece) bool { return s.holds(p, false) }

func (s *Session) issue(p Piece, carried []Piece, text string, authored bool) {
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
	s.call.pending = append(s.call.pending, pendingServe{piece: p, text: text, covers: covers, authored: authored, seq: s.nextSeq()})
}

// ServeChanged reports whether the call serves p, a piece that serves once per
// change of its content: while its content differs from the last the session
// served under p.Name, or when the call serves every piece in full. An empty
// Hash withdraws the content, so the next one serves again; it serves
// nothing. The change is recorded by RecordServed. text is what the agent
// reads of it, which no question's phrase may occur in.
func (s *Session) ServeChanged(p Piece, text string) bool {
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
	s.call.pending = append(s.call.pending, pendingServe{piece: p, text: text, changed: true, seq: s.nextSeq()})
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
			if _, known := s.onceTexts[p.piece]; !known && p.text != "" {
				record["text"] = p.text
			}
		} else if _, known := s.texts[p.piece]; !known {
			record["text"] = p.text
			if p.authored {
				record["authored"] = true
			}
		}
		s.foldText(record)
		records = append(records, record)
	}
	s.call.pending = nil
	data := map[string]any{}
	if s.call.missed {
		s.call.missed = false
		data["missed"] = true
	}
	if len(records) > 0 {
		data["texts"] = records
	}
	if n := s.call.answered - 1; n >= 0 && !s.questions[n].answered {
		data["answered"] = n
		if s.call.short {
			data["short"] = true
		}
		s.foldAnswered(n)
	}
	if len(data) == 0 {
		return
	}
	s.foldOutcome(data)
	s.appendEvent("", EventTexts, data)
}

// foldAnswered marks the round whose first question is n answered.
func (s *Session) foldAnswered(n int) {
	for i := range s.questions {
		if s.questions[i].round == n {
			s.questions[i].answered = true
		}
	}
}

// foldOutcome folds what a call's answer showed: a lost or wrong answer
// right after a call that served short is a late catch, a compaction the
// short serve could not see (20261121-230157-d-tac-ym8).
func (s *Session) foldOutcome(data map[string]any) {
	if data["missed"] == true {
		if s.lastShort {
			s.lateCatches++
		}
		s.lastShort = false
	}
	if _, answered := intValue(data["answered"]); answered {
		s.lastShort = data["short"] == true
	}
}

// LateCatches counts, in a session log, the lost or wrong answers given right
// after a call that served short: each a compaction caught one call late,
// as a right answer to a question asked before it cannot show it.
func LateCatches(events []Event) int {
	s := &Session{}
	for _, ev := range events {
		if ev.Event == EventTexts {
			s.foldTexts(ev.Data)
		}
	}
	return s.lateCatches
}

// DropAnswer keeps the call's answer from counting a question answered, for
// a call that failed: its question stays open for a later call.
func (s *Session) DropAnswer() { s.call.answered, s.call.missed = 0, false }

// Ask ends a response with two questions, one line each: the first about
// the text held longest by its latest copy, the next oldest where that text
// yields no question not asked before; the second about the next held text
// that yields one, or another spot of the first's text where none does. A
// question's phrase occurs in no other text served and in nothing the agent
// or the person wrote, it asks about no text they wrote, and its answer is
// no earlier question's, so an answer replayed from the call history answers
// nothing. It logs the questions and returns their lines, empty when no held
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
	once := make([]string, 0, len(s.onceTexts))
	for _, text := range s.onceTexts {
		once = append(once, strings.ToLower(text))
	}
	elsewhere := func(own Piece, phrase string) bool {
		phrase = strings.ToLower(phrase)
		if phrase == "" {
			return false
		}
		for p, text := range lower {
			if p != own && strings.Contains(text, phrase) {
				return true
			}
		}
		return slices.ContainsFunc(once, func(text string) bool { return strings.Contains(text, phrase) }) || s.inAuthored(phrase)
	}
	answeredBefore := func(answer string) bool {
		return slices.ContainsFunc(s.questions, func(asked askedQuestion) bool { return asked.question.Answered(answer) })
	}
	ask := func(p Piece, first *model.TextQuestion) (model.TextQuestion, bool) {
		held := s.texts[p]
		if held.authored {
			return model.TextQuestion{}, false
		}
		q, ok := model.DrawQuestion(held.text, s.random(), func(q model.TextQuestion) bool {
			if first != nil && (q.Phrase == first.Phrase || q.Phrase == first.Second || q.Second != "" && q.Second == first.Phrase) {
				return false
			}
			return !s.asked[p.key()+"|"+q.Key()] && !elsewhere(p, q.Phrase) && !elsewhere(p, q.Second) && !answeredBefore(q.Answer)
		})
		if !ok {
			return q, false
		}
		data := map[string]any{
			"name": p.Name, "hash": p.Hash, "latest": held.latest,
			"kind": string(q.Kind), "phrase": q.Phrase, "answer": q.Answer,
		}
		if q.Second != "" {
			data["second"] = q.Second
		}
		if first != nil {
			data["paired"] = true
		}
		s.foldQuestion(data)
		s.appendEvent("", EventQuestion, data)
		return q, true
	}
	for i, p := range pieces {
		first, ok := ask(p, nil)
		if !ok {
			continue
		}
		line := first.Line(p.Name)
		for _, other := range append(slices.Delete(slices.Clone(pieces), i, i+1), p) {
			if second, ok := ask(other, &first); ok {
				return line + "\n" + second.Line(other.Name)
			}
		}
		return line
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
	if n, ok := intValue(data["answered"]); ok && n >= 0 {
		s.foldAnswered(n)
	}
	s.foldOutcome(data)
}

func (s *Session) foldText(fields map[string]any) {
	name, _ := fields["name"].(string)
	hash, _ := fields["hash"].(string)
	if fields["changed"] == true {
		if s.changed == nil {
			s.changed = map[string]string{}
		}
		s.changed[name] = hash
		if text, _ := fields["text"].(string); text != "" {
			if s.onceTexts == nil {
				s.onceTexts = map[Piece]string{}
			}
			s.onceTexts[Piece{Name: name, Hash: hash}] = text
		}
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
	s.texts[p] = &heldText{text: text, latest: s.copies, authored: fields["authored"] == true}
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
		round: len(s.questions),
	}
	if n := len(s.questions); data["paired"] == true && n > 0 {
		asked.round = s.questions[n-1].round
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
