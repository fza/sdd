package model_test

import (
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

func TestIgnoredSyncInterval(t *testing.T) {
	cases := []struct {
		name      string
		interval  string
		committed bool
		ignored   bool
	}{
		{name: "unset"},
		{name: "personal layer at the minimum", interval: "1m"},
		{name: "personal layer above the minimum", interval: "10m"},
		{name: "committed layer", interval: "5m", committed: true, ignored: true},
		{name: "malformed", interval: "often", ignored: true},
		{name: "below the minimum", interval: "30s", ignored: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := model.IgnoredSettings(model.BaseConfig{Sync: model.SyncConfig{Interval: tc.interval}}, tc.committed)
			if !tc.ignored {
				if len(got) != 0 {
					t.Errorf("an interval the layer may set should be honored, got %v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Key != "sync.interval" || got[0].Reason == "" {
				t.Errorf("the interval should be reported as ignored with its key and a reason, got %v", got)
			}
		})
	}
}

func TestIgnoredSettingsReportsEveryKey(t *testing.T) {
	got := model.IgnoredSettings(model.BaseConfig{Sync: model.SyncConfig{Mode: model.SyncModeAuto, Interval: "5m"}}, true)
	if len(got) != 2 {
		t.Errorf("both personal-only keys in the committed layer should be reported, got %v", got)
	}
}

func TestResolveSyncInterval(t *testing.T) {
	cases := []struct {
		name     string
		mode     model.SyncMode
		interval string
		want     time.Duration
		on       bool
	}{
		{name: "auto with an interval", mode: model.SyncModeAuto, interval: "5m", want: 5 * time.Minute, on: true},
		{name: "auto at the minimum", mode: model.SyncModeAuto, interval: "1m", want: time.Minute, on: true},
		{name: "auto without an interval", mode: model.SyncModeAuto},
		{name: "manual with an interval", mode: model.SyncModeManual, interval: "5m"},
		{name: "auto below the minimum", mode: model.SyncModeAuto, interval: "59s"},
		{name: "auto with a malformed interval", mode: model.SyncModeAuto, interval: "soon"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Sync: model.SyncConfig{Mode: tc.mode, Interval: tc.interval}}}
			got, on := model.ResolveSyncInterval(cfg)
			if on != tc.on || got != tc.want {
				t.Errorf("the timer should run only under auto with a valid interval, got %v, %t", got, on)
			}
		})
	}
	if _, on := model.ResolveSyncInterval(nil); on {
		t.Error("no config should run no timer")
	}
}

func TestHonoredCommittedDropsTheInterval(t *testing.T) {
	committed := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Sync: model.SyncConfig{Interval: "5m", Cooldown: "5m"}}}
	honored := model.HonoredCommitted(committed)
	if honored.Sync.Interval != "" || honored.Sync.Cooldown != "5m" {
		t.Errorf("only the interval should be dropped from the committed layer, got %+v", honored.Sync)
	}
}

func TestSyncIntervalLayering(t *testing.T) {
	base := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Sync: model.SyncConfig{Interval: "5m"}}}
	if got := model.MergeConfig(base, &model.PerRepoConfig{}).Sync.Interval; got != "5m" {
		t.Errorf("an unset higher layer should keep the lower interval, got %q", got)
	}
	overlay := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Sync: model.SyncConfig{Interval: "2m"}}}
	if got := model.MergeConfig(base, overlay).Sync.Interval; got != "2m" {
		t.Errorf("a higher layer should override the interval, got %q", got)
	}
}
