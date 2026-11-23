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
