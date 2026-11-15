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

func TestResolveReplacedRetention(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{raw: "", want: 14 * 24 * time.Hour},
		{raw: "3d", want: 3 * 24 * time.Hour},
		{raw: "36h", want: 36 * time.Hour},
		{raw: "-1h", wantErr: true},
		{raw: "later", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			got, err := model.ResolveReplacedRetention(model.IndexConfig{ReplacedRetention: tc.raw})
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Errorf("retention resolved to %v, %v; want %v, error %v", got, err, tc.want, tc.wantErr)
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
