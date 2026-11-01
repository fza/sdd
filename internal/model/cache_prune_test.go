package model_test

import (
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

func TestResolveCacheRetention(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{raw: "", want: 90 * 24 * time.Hour},
		{raw: "30d", want: 30 * 24 * time.Hour},
		{raw: "36h", want: 36 * time.Hour},
		{raw: "0d", wantErr: true},
		{raw: "soon", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			got, err := model.ResolveCacheRetention(model.CacheConfig{Retention: tc.raw})
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Errorf("retention resolved to %v, %v; want %v, error %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestCachePruneStatusDue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		status model.CachePruneStatus
		want   bool
	}{
		{name: "never run", want: true},
		{name: "launched an hour ago", status: model.CachePruneStatus{LaunchedAt: now.Add(-time.Hour)}},
		{name: "started an hour ago", status: model.CachePruneStatus{StartedAt: now.Add(-time.Hour), LaunchedAt: now.Add(-48 * time.Hour)}},
		{name: "a day since both", status: model.CachePruneStatus{StartedAt: now.Add(-25 * time.Hour), LaunchedAt: now.Add(-24 * time.Hour)}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.status.Due(now); got != tc.want {
				t.Errorf("due should be %v", tc.want)
			}
		})
	}
}

func TestCachePruneStatusFailureNotice(t *testing.T) {
	t.Parallel()
	failed := model.CachePruneStatus{Error: "permission denied", FinishedAt: time.Now()}
	if notice := failed.FailureNotice(); !strings.Contains(notice, "permission denied") || !strings.Contains(notice, "sdd info") {
		t.Errorf("an unreported failure should name the error and sdd info, got %q", notice)
	}
	failed.Reported = true
	if notice := failed.FailureNotice(); notice != "" {
		t.Errorf("a reported failure should not be shown again, got %q", notice)
	}
	if notice := (model.CachePruneStatus{}).FailureNotice(); notice != "" {
		t.Errorf("a run without failure has no notice, got %q", notice)
	}
}
