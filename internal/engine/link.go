package engine

import "slices"

// CarriedText is a text an earlier session holds, carried into a session
// that links it (20261122-105026-d-tac-wl5).
type CarriedText struct {
	Name string
	Hash string
	Text string
}

// HeldTexts folds a session log's served record: the texts it still counts
// as held, those it carries through its own link included, oldest latest
// copy first. recorded reports whether the log holds a record of served
// texts at all, which a log stock sdd or a build before the text question
// wrote does not.
func HeldTexts(events []Event) (texts []CarriedText, recorded bool) {
	s := &Session{}
	for _, ev := range events {
		switch ev.Event {
		case EventTexts:
			s.foldTexts(ev.Data)
		case EventLinked:
			s.foldLinked(ev.Data)
		}
	}
	held := make([]Piece, 0, len(s.texts))
	for p, text := range s.texts {
		if text.latest >= s.heldFrom {
			held = append(held, p)
		}
	}
	slices.SortFunc(held, func(a, b Piece) int { return s.texts[a].latest - s.texts[b].latest })
	texts = make([]CarriedText, len(held))
	for i, p := range held {
		texts[i] = CarriedText{Name: p.Name, Hash: p.Hash, Text: s.texts[p].text}
	}
	return texts, s.copies > 0
}

// Link records that the session links the earlier session named: the texts
// it carries from it count as held texts served before any of the session's
// own, so a right answer serves a copy of one short.
func (s *Session) Link(session string, texts []CarriedText) {
	records := make([]any, len(texts))
	for i, text := range texts {
		records[i] = map[string]any{"name": text.Name, "hash": text.Hash, "text": text.Text}
	}
	data := map[string]any{"session": session, "texts": records}
	s.foldLinked(data)
	s.appendEvent("", EventLinked, data)
}

func (s *Session) foldLinked(data map[string]any) {
	records, _ := data["texts"].([]any)
	for _, record := range records {
		fields, _ := record.(map[string]any)
		s.foldText(fields)
	}
}
