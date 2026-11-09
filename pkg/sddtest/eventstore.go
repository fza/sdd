package sddtest

import (
	"bytes"
	"slices"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// EventStoreFixture drives the conformance of a project event store. Store
// must start empty.
type EventStoreFixture struct {
	Store sdd.EventStore
}

// RunEventStoreTests checks append and query by channel, type, subject and
// time, the result order, one copy per ID, the refusal of an invalid batch
// and that stored events share no memory with callers.
func RunEventStoreTests(t *testing.T, factory func(*testing.T) EventStoreFixture) {
	t.Helper()
	base := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	events := []sdd.Event{
		{ID: "e3", Channel: "recheck", Type: "sdd.recheck", Time: base.Add(2 * time.Minute), Writer: "sdd", Subject: sdd.EventSubject{Entries: []string{"20261109-004506-d-cpt-udz", "20261107-115150-d-cpt-0mu"}}, Payload: []byte{0x00, 0xff}},
		{ID: "e1", Channel: "outside", Type: "tracker.comment", Time: base, Writer: "Tracker", Subject: sdd.EventSubject{URI: "https://tracker.example/issues/7"}, Payload: []byte(`{"body":"first"}`)},
		{ID: "e2", Channel: "outside", Type: "tracker.closed", Time: base.Add(time.Minute), Writer: "Tracker", Subject: sdd.EventSubject{URI: "https://tracker.example/issues/7", Entries: []string{"20261109-004506-d-cpt-udz"}}},
		{ID: "e0", Channel: "outside", Type: "tracker.comment", Time: base.Add(2 * time.Minute), Writer: "Tracker", Subject: sdd.EventSubject{URI: "https://tracker.example/issues/8"}, Payload: []byte(`{"body":"other"}`)},
	}
	byID := map[string]sdd.Event{}
	for _, event := range events {
		byID[event.ID] = event
	}

	t.Run("empty store", func(t *testing.T) {
		store := factory(t).Store
		got, err := store.Query(t.Context(), sdd.EventQuery{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("an empty store returned %d events", len(got))
		}
		err = store.Append(t.Context(), nil)
		if err != nil {
			t.Fatalf("appending no events: %v", err)
		}
	})

	t.Run("query", func(t *testing.T) {
		store := factory(t).Store
		err := store.Append(t.Context(), events[:2])
		if err != nil {
			t.Fatal(err)
		}
		err = store.Append(t.Context(), events[2:])
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			name  string
			query sdd.EventQuery
			want  []string
		}{
			{name: "everything, by time then ID", query: sdd.EventQuery{}, want: []string{"e1", "e2", "e0", "e3"}},
			{name: "channel", query: sdd.EventQuery{Channels: []string{"recheck"}}, want: []string{"e3"}},
			{name: "type", query: sdd.EventQuery{Types: []string{"tracker.comment"}}, want: []string{"e1", "e0"}},
			{name: "types", query: sdd.EventQuery{Types: []string{"tracker.closed", "sdd.recheck"}}, want: []string{"e2", "e3"}},
			{name: "subject URI", query: sdd.EventQuery{URI: "https://tracker.example/issues/7"}, want: []string{"e1", "e2"}},
			{name: "subject entry", query: sdd.EventQuery{Entries: []string{"20261109-004506-d-cpt-udz"}}, want: []string{"e2", "e3"}},
			{name: "any subject entry", query: sdd.EventQuery{Entries: []string{"20261107-115150-d-cpt-0mu", "20260101-000000-s-tac-xyz"}}, want: []string{"e3"}},
			{name: "since is inclusive", query: sdd.EventQuery{Since: base.Add(time.Minute)}, want: []string{"e2", "e0", "e3"}},
			{name: "until is exclusive", query: sdd.EventQuery{Until: base.Add(time.Minute)}, want: []string{"e1"}},
			{name: "fields combine", query: sdd.EventQuery{URI: "https://tracker.example/issues/7", Entries: []string{"20261109-004506-d-cpt-udz"}, Since: base}, want: []string{"e2"}},
			{name: "no match", query: sdd.EventQuery{Types: []string{"sdd.wip"}}, want: nil},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got, err := store.Query(t.Context(), tc.query)
				if err != nil {
					t.Fatal(err)
				}
				if ids := eventIDs(got); !slices.Equal(ids, tc.want) {
					t.Fatalf("Query = %v, want %v", ids, tc.want)
				}
				for _, event := range got {
					assertEventEqual(t, event, byID[event.ID])
				}
			})
		}
	})

	t.Run("one copy per ID", func(t *testing.T) {
		store := factory(t).Store
		err := store.Append(t.Context(), events[1:2])
		if err != nil {
			t.Fatal(err)
		}
		err = store.Append(t.Context(), events[1:2])
		if err != nil {
			t.Fatalf("appending a stored ID again: %v", err)
		}
		got, err := store.Query(t.Context(), sdd.EventQuery{})
		if err != nil {
			t.Fatal(err)
		}
		if ids := eventIDs(got); !slices.Equal(ids, []string{"e1"}) {
			t.Fatalf("Query after a repeated append = %v, want a single copy", ids)
		}
	})

	t.Run("invalid batch stores nothing", func(t *testing.T) {
		store := factory(t).Store
		invalid := events[0]
		invalid.ID = "e9"
		invalid.Writer = ""
		err := store.Append(t.Context(), []sdd.Event{events[1], invalid})
		if err == nil {
			t.Fatal("Append accepted an event without a writer")
		}
		got, err := store.Query(t.Context(), sdd.EventQuery{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("a refused batch left %v stored", eventIDs(got))
		}
	})

	t.Run("no shared memory", func(t *testing.T) {
		store := factory(t).Store
		input := events[0]
		input.Payload = bytes.Clone(events[0].Payload)
		input.Subject.Entries = slices.Clone(events[0].Subject.Entries)
		err := store.Append(t.Context(), []sdd.Event{input})
		if err != nil {
			t.Fatal(err)
		}
		input.Payload[0] = 'x'
		input.Subject.Entries[0] = "changed"
		first, err := store.Query(t.Context(), sdd.EventQuery{})
		if err != nil {
			t.Fatal(err)
		}
		if len(first) != 1 {
			t.Fatalf("Query returned %d events", len(first))
		}
		assertEventEqual(t, first[0], events[0])
		first[0].Payload[0] = 'y'
		first[0].Subject.Entries[0] = "changed"
		second, err := store.Query(t.Context(), sdd.EventQuery{})
		if err != nil {
			t.Fatal(err)
		}
		assertEventEqual(t, second[0], events[0])
	})
}

func eventIDs(events []sdd.Event) []string {
	var ids []string
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	return ids
}

func assertEventEqual(t *testing.T, got, want sdd.Event) {
	t.Helper()
	if got.ID != want.ID || got.Channel != want.Channel || got.Type != want.Type || !got.Time.Equal(want.Time) || got.Writer != want.Writer ||
		got.Subject.URI != want.Subject.URI || !slices.Equal(got.Subject.Entries, want.Subject.Entries) || !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("stored event = %+v, want %+v", got, want)
	}
}
