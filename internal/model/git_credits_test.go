package model_test

import (
	"slices"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

func TestGitFetchSlice(t *testing.T) {
	cases := []struct {
		name       string
		configured time.Duration
		perHour    int
		repos      int
		want       time.Duration
	}{
		{name: "few repositories keep the configured cooldown", configured: 5 * time.Minute, perHour: 600, repos: 10, want: 5 * time.Minute},
		{name: "at the bucket's share the slice equals the cooldown", configured: 5 * time.Minute, perHour: 600, repos: 50, want: 5 * time.Minute},
		{name: "more repositories stretch the slice", configured: 5 * time.Minute, perHour: 600, repos: 100, want: 10 * time.Minute},
		{name: "a small bucket stretches it past an hour", configured: 5 * time.Minute, perHour: 60, repos: 120, want: 2 * time.Hour},
		{name: "a slice of seconds", configured: time.Minute, perHour: 3600, repos: 90, want: 90 * time.Second},
		{name: "no repository tracked yet", configured: 5 * time.Minute, perHour: 600, repos: 0, want: 5 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := model.GitFetchSlice(tc.configured, tc.perHour, tc.repos)
			if got != tc.want {
				t.Errorf("the slice should keep the repositories' fetches within the hourly limit, got %v", got)
			}
		})
	}
}

func TestIdleGitSlices(t *testing.T) {
	base := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	at := func(minutes int) time.Time { return base.Add(time.Duration(minutes) * time.Minute) }
	cases := []struct {
		name    string
		due     time.Time
		period  time.Duration
		since   time.Time
		now     time.Time
		credits []time.Time
		current time.Time
	}{
		{name: "slice not yet due", due: at(5), period: 5 * time.Minute, since: at(-59), now: at(3), current: at(5)},
		{name: "slice in progress earns nothing", due: at(5), period: 5 * time.Minute, since: at(-59), now: at(9), current: at(5)},
		{name: "a slice ending unused earns one credit dated when it came due", due: at(5), period: 5 * time.Minute, since: at(-49), now: at(10), credits: []time.Time{at(5)}, current: at(10)},
		{name: "every unused slice earns one", due: at(5), period: 5 * time.Minute, since: at(-40), now: at(22), credits: []time.Time{at(5), at(10), at(15)}, current: at(20)},
		{name: "credits dated before the window are dropped", due: at(0), period: 10 * time.Minute, since: at(25), now: at(59), credits: []time.Time{at(30), at(40)}, current: at(50)},
		{name: "a long idle stretch jumps to the window", due: at(0), period: time.Minute, since: at(10_000), now: at(10_003), credits: []time.Time{at(10_000), at(10_001), at(10_002)}, current: at(10_003)},
		{name: "no slice recorded", period: 5 * time.Minute, since: at(-59), now: at(60)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			credits, current := model.IdleGitSlices(tc.due, tc.period, tc.since, tc.now)
			if !slices.EqualFunc(credits, tc.credits, time.Time.Equal) {
				t.Errorf("each slice that ended unused within the window should earn one credit, got %v", credits)
			}
			if !current.Equal(tc.current) {
				t.Errorf("the slice should roll forward to the one in progress, got %v", current)
			}
		})
	}
}
