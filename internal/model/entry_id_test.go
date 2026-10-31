package model_test

import (
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

func TestEntryIDTimeRoundTripsInLocalTime(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("UTC+1", 3600)
	t.Cleanup(func() { time.Local = previousLocal })

	captured := time.Date(2026, 10, 31, 23, 37, 10, 0, time.Local)

	t.Run("entry ID", func(t *testing.T) {
		id := model.GenerateIDAt(model.TypeSignal, model.LayerTactical, "abc", captured)
		parts, err := model.ParseID(id)
		if err != nil {
			t.Fatalf("parsing a generated ID: %v", err)
		}
		if !parts.Time.Equal(captured) {
			t.Errorf("parsed time should be the instant the ID was generated at, got %v", parts.Time)
		}
	})

	t.Run("WIP marker", func(t *testing.T) {
		filename := captured.Format("20060102-150405") + "-felix.md"
		marker, err := model.ParseWIPMarker(filename, "---\nentry: 20261031-120000-d-tac-abc\nparticipant: Felix\n---\n")
		if err != nil {
			t.Fatalf("parsing a marker: %v", err)
		}
		if !marker.Time.Equal(captured) {
			t.Errorf("parsed time should be the instant the marker was created at, got %v", marker.Time)
		}
	})
}
