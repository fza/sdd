package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// The retired sync.interval is read from no layer, so no value in any layer
// warns or fails: another sdd version on the machine may still read it.
func TestSyncIntervalIsNeverReported(t *testing.T) {
	for _, source := range []model.ConfigValueSource{model.ConfigSourceGlobal, model.ConfigSourceProject, model.ConfigSourceLocal} {
		for _, interval := range []string{"5m", "often", "30s"} {
			t.Run(string(source)+" "+interval, func(t *testing.T) {
				got := model.IgnoredSettings(model.BaseConfig{Sync: model.SyncConfig{Interval: interval}}, source)
				if len(got) != 0 {
					t.Errorf("the retired interval should raise no warning, got %v", got)
				}
			})
		}
	}
}

func TestIgnoredSettingsReportsEveryKey(t *testing.T) {
	got := model.IgnoredSettings(model.BaseConfig{Sync: model.SyncConfig{Mode: model.SyncModeAuto}, Dialogue: model.DialogueConfig{Mode: model.DialogueAuto}}, model.ConfigSourceProject)
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
