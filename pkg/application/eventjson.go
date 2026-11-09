package application

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// eventJSON is one line of the JSON form of the event store
// (20261109-004506-d-cpt-udz, D16). A payload that is compact JSON is shown
// as itself, any other payload as base64, so both survive a round trip byte
// for byte.
type eventJSON struct {
	ID            string           `json:"id"`
	Channel       string           `json:"channel"`
	Type          string           `json:"type"`
	Time          string           `json:"time"`
	Writer        string           `json:"writer"`
	Subject       eventSubjectJSON `json:"subject"`
	Payload       json.RawMessage  `json:"payload,omitempty"`
	PayloadBase64 string           `json:"payload_base64,omitempty"`
}

type eventSubjectJSON struct {
	URI     string   `json:"uri,omitempty"`
	Entries []string `json:"entries,omitempty"`
}

// ExportEventsJSON writes the events the query selects as JSON Lines, one
// event per line in query order.
func ExportEventsJSON(ctx context.Context, store EventStore, query EventQuery, w io.Writer) error {
	events, err := store.Query(ctx, query)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	for _, event := range events {
		line := eventJSON{
			ID: event.ID, Channel: event.Channel, Type: event.Type, Time: event.Time.UTC().Format(time.RFC3339Nano), Writer: event.Writer,
			Subject: eventSubjectJSON{URI: event.Subject.URI, Entries: event.Subject.Entries},
		}
		if compactJSON(event.Payload) {
			line.Payload = event.Payload
		} else if len(event.Payload) > 0 {
			line.PayloadBase64 = base64.StdEncoding.EncodeToString(event.Payload)
		}
		err := encoder.Encode(line)
		if err != nil {
			return fmt.Errorf("writing event %s: %w", event.ID, err)
		}
	}
	return nil
}

func compactJSON(payload []byte) bool {
	if len(payload) == 0 || !json.Valid(payload) {
		return false
	}
	var compacted bytes.Buffer
	err := json.Compact(&compacted, payload)
	return err == nil && bytes.Equal(compacted.Bytes(), payload)
}

// ImportEventsJSON reads JSON Lines as ExportEventsJSON writes them and
// appends the events the store does not hold yet, returning how many it
// appended. Every line is checked before anything is appended.
func ImportEventsJSON(ctx context.Context, store EventStore, r io.Reader) (int, error) {
	var events []Event
	scanner := bufio.NewScanner(r)
	scanner.Buffer(nil, 64<<20)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		event, err := decodeEventJSON(line)
		if err != nil {
			return 0, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		events = append(events, event)
	}
	err := scanner.Err()
	if err != nil {
		return 0, fmt.Errorf("reading events: %w", err)
	}
	stored, err := store.Query(ctx, EventQuery{})
	if err != nil {
		return 0, err
	}
	known := make(map[string]bool, len(stored))
	for _, event := range stored {
		known[event.ID] = true
	}
	var fresh []Event
	for _, event := range events {
		if !known[event.ID] {
			known[event.ID] = true
			fresh = append(fresh, event)
		}
	}
	if len(fresh) == 0 {
		return 0, nil
	}
	err = store.Append(ctx, fresh)
	if err != nil {
		return 0, err
	}
	return len(fresh), nil
}

func decodeEventJSON(line []byte) (Event, error) {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	var decoded eventJSON
	err := decoder.Decode(&decoded)
	if err != nil {
		return Event{}, err
	}
	if decoder.More() {
		return Event{}, errors.New("more than one JSON value on the line")
	}
	at, err := time.Parse(time.RFC3339Nano, decoded.Time)
	if err != nil {
		return Event{}, fmt.Errorf("event %s: time: %w", decoded.ID, err)
	}
	event := Event{
		ID: decoded.ID, Channel: decoded.Channel, Type: decoded.Type, Time: at.UTC(), Writer: decoded.Writer,
		Subject: EventSubject{URI: decoded.Subject.URI, Entries: decoded.Subject.Entries},
	}
	switch {
	case len(decoded.Payload) > 0 && decoded.PayloadBase64 != "":
		return Event{}, fmt.Errorf("event %s: payload and payload_base64 are both set", decoded.ID)
	case len(decoded.Payload) > 0:
		event.Payload = []byte(decoded.Payload)
	case decoded.PayloadBase64 != "":
		event.Payload, err = base64.StdEncoding.DecodeString(decoded.PayloadBase64)
		if err != nil {
			return Event{}, fmt.Errorf("event %s: payload_base64: %w", decoded.ID, err)
		}
	}
	err = ValidateEvent(event)
	if err != nil {
		return Event{}, err
	}
	return event, nil
}
