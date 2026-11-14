package model_test

import (
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

func TestRoundDeadline(t *testing.T) {
	launched := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		previousEnded time.Time
		want          time.Time
	}{
		{name: "first round", want: launched.Add(time.Minute)},
		{name: "previous round ended before the launch", previousEnded: launched.Add(-time.Hour), want: launched.Add(time.Minute)},
		{name: "previous round ended after the launch", previousEnded: launched.Add(5 * time.Minute), want: launched.Add(6 * time.Minute)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := model.RoundDeadline(launched, tt.previousEnded, time.Minute); !got.Equal(tt.want) {
				t.Fatalf("RoundDeadline() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestLapsedRound(t *testing.T) {
	deadline := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		alive bool
		now   time.Time
		want  model.RoundLapse
	}{
		{name: "running in time", alive: true, now: deadline, want: model.RoundRunning},
		{name: "running within the margin", alive: true, now: deadline.Add(model.RoundOutcomeMargin), want: model.RoundRunning},
		{name: "running past the margin", alive: true, now: deadline.Add(model.RoundOutcomeMargin + time.Second), want: model.RoundPastTimeout},
		{name: "process gone in time", alive: false, now: deadline, want: model.RoundProcessGone},
		{name: "process gone past the margin", alive: false, now: deadline.Add(time.Hour), want: model.RoundProcessGone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := model.LapsedRound(tt.alive, deadline, tt.now); got != tt.want {
				t.Fatalf("LapsedRound() = %d, want %d", got, tt.want)
			}
		})
	}
}
