package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

var retiredSyncLayers = []struct {
	key   string
	value string
	sync  model.SyncConfig
}{
	{key: "sync.mode", value: "auto", sync: model.SyncConfig{Mode: model.SyncModeAuto}},
	{key: "sync.mode", value: "always", sync: model.SyncConfig{Mode: "always"}},
	{key: "sync.interval", value: "5m", sync: model.SyncConfig{Interval: "5m"}},
	{key: "sync.interval", value: "often", sync: model.SyncConfig{Interval: "often"}},
	{key: "sync.cooldown", value: "15m", sync: model.SyncConfig{Cooldown: "15m"}},
	{key: "sync.cooldown", value: "soon", sync: model.SyncConfig{Cooldown: "soon"}},
}

// The retired sync keys are read from no layer, so no value in any layer
// warns or fails: another sdd version on the machine may still read them.
func TestRetiredSyncKeysAreNeverReported(t *testing.T) {
	for _, source := range []model.ConfigValueSource{model.ConfigSourceGlobal, model.ConfigSourceProject, model.ConfigSourceLocal} {
		for _, tc := range retiredSyncLayers {
			t.Run(string(source)+" "+tc.key+" "+tc.value, func(t *testing.T) {
				got := model.IgnoredSettings(model.BaseConfig{Sync: tc.sync}, source)
				if len(got) != 0 {
					t.Errorf("a retired sync key should raise no warning, got %v", got)
				}
			})
		}
	}
}

func TestRetiredSyncKeysAreNotEffective(t *testing.T) {
	unset := model.EffectiveConfigValues(model.BaseConfig{}, nil, nil)
	for _, tc := range retiredSyncLayers {
		t.Run(tc.key+" "+tc.value, func(t *testing.T) {
			layer := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Sync: tc.sync}}
			for _, values := range [][]model.ConfigValue{unset, model.EffectiveConfigValues(model.BaseConfig{Sync: tc.sync}, layer, layer)} {
				for _, v := range values {
					if v.Key == tc.key {
						t.Errorf("a retired sync key should not show as effective, got %+v", v)
					}
				}
			}
		})
	}
}

func TestIgnoredSettingsReportsEveryKey(t *testing.T) {
	debug := true
	got := model.IgnoredSettings(model.BaseConfig{Dialogue: model.DialogueConfig{Mode: model.DialogueAuto}, LLM: model.LLMConfig{Debug: &debug}}, model.ConfigSourceProject)
	if len(got) != 2 {
		t.Errorf("both personal-only keys in the committed layer should be reported, got %v", got)
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
