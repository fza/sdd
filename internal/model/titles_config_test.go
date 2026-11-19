package model_test

import (
	"strconv"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestTitlesBackfillIsOffUnlessALayerSwitchesItOn(t *testing.T) {
	on, off := true, false
	committed := func(value *bool) *model.PerRepoConfig {
		cfg := &model.PerRepoConfig{}
		cfg.Titles.Backfill = value
		return cfg
	}
	for _, tc := range []struct {
		name           string
		global         model.BaseConfig
		project, local *model.PerRepoConfig
		want           bool
		source         model.ConfigValueSource
	}{
		{name: "no layer sets it", source: model.ConfigSourceDefault},
		{name: "the user-global config switches it on", global: committed(&on).BaseConfig, want: true, source: model.ConfigSourceGlobal},
		{name: "the committed project config switches it on", project: committed(&on), want: true, source: model.ConfigSourceProject},
		{name: "the local config switches it off for one checkout", project: committed(&on), local: committed(&off), source: model.ConfigSourceLocal},
		{name: "the project config switches it off under a global on", global: committed(&on).BaseConfig, project: committed(&off), source: model.ConfigSourceProject},
	} {
		t.Run(tc.name, func(t *testing.T) {
			merged := model.MergeConfig(model.MergeConfig(&model.PerRepoConfig{BaseConfig: tc.global}, tc.project), tc.local)
			if merged.Titles.Backfills() != tc.want {
				t.Errorf("the highest layer setting the switch should decide it, got %v", merged.Titles.Backfills())
			}
			shown := effective(model.EffectiveConfigValues(tc.global, tc.project, tc.local))["titles.backfill"]
			if shown.Value != strconv.FormatBool(tc.want) || shown.Source != tc.source {
				t.Errorf("sdd config should show the effective value and the layer it comes from, got %+v", shown)
			}
		})
	}
}
