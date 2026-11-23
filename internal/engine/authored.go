package engine

import (
	"maps"
	"slices"
	"strings"
)

// authoredFields names, per event, the fields that carry what the agent or
// the person wrote: reports, chooser answers with the person's words, park
// reasons, deliveries between agents and start inputs. sdd echoes these
// back in templates, and a compaction summary quotes them, so no question
// asks about them (20261121-230157-d-tac-ym8).
var authoredFields = map[EventType][]string{
	EventStarted:       {"params"},
	EventReport:        {"fields"},
	EventChooserAnswer: {"fields", "userWords", "reason"},
	EventParked:        {"note", "reason"},
	EventDelivered:     {"fields"},
	EventReceived:      {"fields"},
}

// noteAuthored folds the strings an event carries of what the agent or the
// person wrote.
func (s *Session) noteAuthored(typ EventType, data map[string]any) {
	for _, field := range authoredFields[typ] {
		collectStrings(data[field], s.addAuthored)
	}
}

func (s *Session) addAuthored(text string) {
	norm := authoredForm(text)
	if strings.Count(norm, " ") < 2 {
		return
	}
	if s.authored == nil {
		s.authored = map[string]bool{}
	}
	s.authored[norm] = true
}

// authoredForm is text as a phrase is looked up in it: lower case, every run
// of white space one space.
func authoredForm(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(text)), " ")
}

func collectStrings(v any, add func(string)) {
	switch value := v.(type) {
	case string:
		add(value)
	case []any:
		for _, item := range value {
			collectStrings(item, add)
		}
	case []string:
		for _, item := range value {
			add(item)
		}
	case map[string]any:
		for _, item := range value {
			collectStrings(item, add)
		}
	}
}

// inAuthored reports whether phrase, single-spaced, occurs in what the agent
// or the person wrote.
func (s *Session) inAuthored(phrase string) bool {
	phrase = authoredForm(phrase)
	for text := range s.authored {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// AuthoredTexts folds what the agent and the person wrote into a session
// log, carried into a session that links it.
func AuthoredTexts(events []Event) []string {
	s := &Session{}
	for _, ev := range events {
		s.noteAuthored(ev.Event, ev.Data)
		if ev.Event == EventLinked {
			s.foldLinked(ev.Data)
		}
	}
	return slices.Sorted(maps.Keys(s.authored))
}
