package model_test

import (
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
		{Event: "capture.assemble.left", Command: "lint-assemble", Findings: true},
		{Event: "capture.guide.left", Command: "lint-guide"},
	}}}

	var hooks []model.ConfigValue
	for _, value := range model.EffectiveConfigValues(global, &model.PerRepoConfig{}, local) {
		if value.Key == "hooks" || strings.HasPrefix(value.Key, "hooks.") {
			hooks = append(hooks, value)
		}
	}
	want := []struct {
		key, command string
		source       model.ConfigValueSource
	}{
		{"hooks.0", "notify", model.ConfigSourceGlobal},
		{"hooks.1", "lint-assemble", model.ConfigSourceLocal},
		{"hooks.2", "lint-guide", model.ConfigSourceLocal},
	}
	if len(hooks) != len(want) {
		t.Fatalf("every hook of every layer should be listed, got %+v", hooks)
	}
	for i, hook := range hooks {
		if hook.Key != want[i].key || hook.Source != want[i].source || !strings.Contains(hook.Value, want[i].command) {
			t.Errorf("each hook should be listed in run order with its command and its layer, got %+v", hook)
		}
	}
}
