package test

import (
	_ "embed"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// LegacyEventSegment is a binary event segment of the recheck channel, as
// sdd wrote them before its event store turned to JSON Lines. It holds
// LegacyEvents.
//
//go:embed fixtures/events/legacy-recheck.seg
var LegacyEventSegment []byte

// LegacyEvents returns the events LegacyEventSegment holds.
func LegacyEvents() []sdd.Event {
	return []sdd.Event{
		{
			ID: "legacy-1", Channel: "recheck", Type: "sdd.recheck", Time: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC), Writer: "sdd",
			Subject: sdd.EventSubject{Entries: []string{"20261109-004506-d-cpt-udz", "20261107-115150-d-cpt-0mu"}},
			Payload: []byte(`{"kind":"strain","relation":"contradicts"}`),
		},
		{
			ID: "legacy-2", Channel: "recheck", Type: "sdd.recheck", Time: time.Date(2026, 11, 9, 12, 1, 0, 500_000_000, time.UTC), Writer: "Local Author",
			Subject: sdd.EventSubject{URI: "https://tracker.example/issues/7"},
			Payload: []byte{0x00, 0xff},
		},
	}
}
