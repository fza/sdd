package model_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// Hooks concatenate across the layers, so the provenance lists every
// layer's hooks in the order they run, each with its own layer
// (20261109-005138-d-tac-i61).
func TestEffectiveConfigValuesListsTheHooksOfEveryLayer(t *testing.T) {
	global := model.BaseConfig{Hooks: []model.HookConfig{{Event: "entry.written", Command: "notify"}}}
	local := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Hooks: []model.HookConfig{
		{Event: "capture.assemble.left", Command: "lint-assemble", Timeout: 30, Findings: true},
		{Event: "capture.guide.left", Command: "lint-guide"},
	}}}

	got := hookValues(model.EffectiveConfigValues(global, &model.PerRepoConfig{}, local))
	want := []model.ConfigValue{
		{Key: "hooks.0.event", Value: "entry.written", Source: model.ConfigSourceGlobal},
		{Key: "hooks.0.command", Value: "notify", Source: model.ConfigSourceGlobal},
		{Key: "hooks.1.event", Value: "capture.assemble.left", Source: model.ConfigSourceLocal},
		{Key: "hooks.1.command", Value: "lint-assemble", Source: model.ConfigSourceLocal},
		{Key: "hooks.1.timeout", Value: "30", Source: model.ConfigSourceLocal},
		{Key: "hooks.1.findings", Value: "true", Source: model.ConfigSourceLocal},
		{Key: "hooks.2.event", Value: "capture.guide.left", Source: model.ConfigSourceLocal},
		{Key: "hooks.2.command", Value: "lint-guide", Source: model.ConfigSourceLocal},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("each hook should be listed in run order by the keys it sets in the config, each with its layer\ngot  %+v\nwant %+v", got, want)
	}
}

func hookValues(values []model.ConfigValue) []model.ConfigValue {
	var hooks []model.ConfigValue
	for _, value := range values {
		if value.Key == "hooks" || strings.HasPrefix(value.Key, "hooks.") {
			hooks = append(hooks, value)
		}
	}
	return hooks
}
