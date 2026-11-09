package application

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/networkteam/sdd/internal/model"
)

// Event is one occurrence in the project event store
// (20261107-115150-d-cpt-0mu): the envelope of type, time, writer, subject
// and payload. ID dedupes copies of one event; Channel names the channel its
// type belongs to, which decides how its storage is shared
// (20261109-004506-d-cpt-udz, D17 and D19). The payload is opaque here: the
// writer of a type owns its shape and retention.
type Event struct {
	ID      string
	Channel string
	Type    string
	Time    time.Time
	Writer  string
	Subject EventSubject
	Payload []byte
}

// EventSubject names what an event is about: an external identity URI, entry
// IDs, or both, so events and entries about one outside item join.
type EventSubject struct {
	URI     string
	Entries []string
}

// EventQuery selects stored events. Each set field narrows the result; a
// list matches any of its values. Since is inclusive, Until exclusive.
type EventQuery struct {
	Channels []string
	Types    []string
	URI      string
	Entries  []string
	Since    time.Time
	Until    time.Time
}

// EventStore is the project event store port. Append keeps every event it
// accepts and rewrites nothing already stored; an event whose ID is already
// stored keeps a single copy in query results. Query returns the matching
// events ordered by time, then ID. Implementations validate every event with
// ValidateEvent and store none of a call's events when one is invalid.
type EventStore interface {
	Append(ctx context.Context, events []Event) error
	Query(ctx context.Context, query EventQuery) ([]Event, error)
}

const maxEventID = 128

// ValidateEvent checks that an event carries a complete envelope.
func ValidateEvent(event Event) error {
	if event.ID == "" || len(event.ID) > maxEventID || strings.ContainsFunc(event.ID, invalidEventIDRune) {
		return fmt.Errorf("event ID %q: want 1 to %d letters, digits, '.', '_', ':' or '-'", event.ID, maxEventID)
	}
	err := model.ValidateEventChannel(event.Channel)
	if err != nil {
		return fmt.Errorf("event %s: %w", event.ID, err)
	}
	err = model.ValidateEventType(event.Type)
	if err != nil {
		return fmt.Errorf("event %s: %w", event.ID, err)
	}
	if event.Time.IsZero() || !time.Unix(0, event.Time.UnixNano()).Equal(event.Time) {
		return fmt.Errorf("event %s: time is missing or outside the years 1678 to 2262", event.ID)
	}
	if strings.TrimSpace(event.Writer) == "" {
		return fmt.Errorf("event %s: writer is required", event.ID)
	}
	if !utf8.ValidString(event.Writer) || !utf8.ValidString(event.Subject.URI) {
		return fmt.Errorf("event %s: writer and subject URI must be valid UTF-8", event.ID)
	}
	if event.Subject.URI == "" && len(event.Subject.Entries) == 0 {
		return fmt.Errorf("event %s: subject needs a URI, entry IDs, or both", event.ID)
	}
	if event.Subject.URI != "" {
		uri, err := url.Parse(event.Subject.URI)
		if err != nil || !uri.IsAbs() {
			return fmt.Errorf("event %s: subject URI %q is not an absolute URI", event.ID, event.Subject.URI)
		}
	}
	for _, entry := range event.Subject.Entries {
		if entry == "" || !utf8.ValidString(entry) || strings.ContainsFunc(entry, isSpace) {
			return fmt.Errorf("event %s: subject entry ID %q is empty, holds spaces or is not UTF-8", event.ID, entry)
		}
	}
	return nil
}

func invalidEventIDRune(r rune) bool {
	return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("._:-", r)
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

// Matches reports whether the query selects the event.
func (q EventQuery) Matches(event Event) bool {
	if len(q.Channels) > 0 && !slices.Contains(q.Channels, event.Channel) {
		return false
	}
	if len(q.Types) > 0 && !slices.Contains(q.Types, event.Type) {
		return false
	}
	if q.URI != "" && event.Subject.URI != q.URI {
		return false
	}
	if len(q.Entries) > 0 && !slices.ContainsFunc(event.Subject.Entries, func(entry string) bool { return slices.Contains(q.Entries, entry) }) {
		return false
	}
	if !q.Since.IsZero() && event.Time.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && !event.Time.Before(q.Until) {
		return false
	}
	return true
}

// SelectEvents returns the events the query matches, one per ID, ordered by
// time and then ID: the result contract of EventStore.Query over events in
// storage order. The first copy of an ID wins.
func SelectEvents(events []Event, query EventQuery) []Event {
	seen := map[string]bool{}
	var selected []Event
	for _, event := range events {
		if seen[event.ID] {
			continue
		}
		seen[event.ID] = true
		if query.Matches(event) {
			selected = append(selected, event)
		}
	}
	slices.SortStableFunc(selected, func(a, b Event) int {
		order := a.Time.Compare(b.Time)
		if order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	return selected
}

// AppendEventsRequest appends events a writer outside core reports into one
// channel.
type AppendEventsRequest struct {
	Channel string
	Events  []EventDraft
}

// EventDraft is the writer's part of an event; the application stamps ID,
// time and writer.
type EventDraft struct {
	Type    string
	Subject EventSubject
	Payload []byte
}

// AppendEvents stores events a writer outside core reports, stamped with a
// fresh ID, the current time and the participant the principal appears as.
// Types under the reserved sdd. namespace are refused: only core writes them.
func (a *Application) AppendEvents(ctx context.Context, identity RequestIdentity, project ProjectID, request AppendEventsRequest) ([]Event, error) {
	principal, runtime, err := a.resolve(ctx, identity, project, AccessWrite)
	if err != nil {
		return nil, err
	}
	store, err := runtime.eventStore()
	if err != nil {
		return nil, err
	}
	writer, err := a.participantFor(ctx, principal, runtime)
	if err != nil {
		return nil, err
	}
	now := a.clock.Now().UTC()
	events := make([]Event, 0, len(request.Events))
	for _, draft := range request.Events {
		if model.EventTypeReserved(draft.Type) {
			return nil, &ApplicationError{Code: ErrorInvalidArgument, Message: fmt.Sprintf("event type %q lies in the reserved %s. namespace, which only sdd itself writes", draft.Type, model.ReservedEventNamespace)}
		}
		event := Event{
			ID: rand.Text(), Channel: request.Channel, Type: draft.Type, Time: now, Writer: writer,
			Subject: EventSubject{URI: draft.Subject.URI, Entries: slices.Clone(draft.Subject.Entries)}, Payload: slices.Clone(draft.Payload),
		}
		err := ValidateEvent(event)
		if err != nil {
			return nil, &ApplicationError{Code: ErrorInvalidArgument, Message: err.Error()}
		}
		events = append(events, event)
	}
	err = store.Append(ctx, events)
	if err != nil {
		return nil, fmt.Errorf("appending events: %w", err)
	}
	return events, nil
}

// QueryEvents returns the project's stored events the query selects.
func (a *Application) QueryEvents(ctx context.Context, identity RequestIdentity, project ProjectID, query EventQuery) ([]Event, error) {
	_, runtime, err := a.resolve(ctx, identity, project, AccessRead)
	if err != nil {
		return nil, err
	}
	store, err := runtime.eventStore()
	if err != nil {
		return nil, err
	}
	events, err := store.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying events: %w", err)
	}
	return events, nil
}

func (r *ProjectRuntime) eventStore() (EventStore, error) {
	if r.options.Events == nil {
		return nil, &ApplicationError{Code: ErrorProjectUnavailable, Message: fmt.Sprintf("project %s has no event store configured", r.options.Project.ID)}
	}
	return r.options.Events, nil
}
