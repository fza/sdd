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
		{Event: "capture.assemble", Command: "lint-assemble", Timeout: 30, Findings: true},
		{Event: "capture.guide", Command: "lint-guide"},
	}}}

	got := hookValues(model.EffectiveConfigValues(global, &model.PerRepoConfig{}, local))
	want := []model.ConfigValue{
		{Key: "hooks.0.event", Value: "entry.written", Source: model.ConfigSourceGlobal},
		{Key: "hooks.0.command", Value: "notify", Source: model.ConfigSourceGlobal},
		{Key: "hooks.1.event", Value: "capture.assemble", Source: model.ConfigSourceLocal},
		{Key: "hooks.1.command", Value: "lint-assemble", Source: model.ConfigSourceLocal},
		{Key: "hooks.1.timeout", Value: "30", Source: model.ConfigSourceLocal},
		{Key: "hooks.1.findings", Value: "true", Source: model.ConfigSourceLocal},
		{Key: "hooks.2.event", Value: "capture.guide", Source: model.ConfigSourceLocal},
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

// A project_dir layout's work repository lists its hooks between the global
// layer's and the project's (20261109-213250-d-tac-bve).
func TestEffectiveConfigValuesListsTheWorkRepositorysHooksBeforeTheProjects(t *testing.T) {
	layer := func(command string) *model.PerRepoConfig {
		return &model.PerRepoConfig{BaseConfig: model.BaseConfig{Hooks: []model.HookConfig{{Event: "capture.assemble", Command: command, Args: []string{"--strict"}}}}}
	}
	work := model.WorkConfigLayers{Committed: layer("work.sh"), Local: layer("local/work.sh")}
	got := hookValues(model.EffectiveConfigValuesWithWork(model.BaseConfig{}, work, layer("project.sh"), layer("local/project.sh")))
	var commands []string
	for _, value := range got {
		if strings.HasSuffix(value.Key, ".command") {
			commands = append(commands, value.Key+"="+value.Value+"("+string(value.Source)+")")
		}
	}
	want := []string{"hooks.0.command=work.sh(work)", "hooks.1.command=local/work.sh(work local)", "hooks.2.command=project.sh(project)", "hooks.3.command=local/project.sh(local)"}
	if !reflect.DeepEqual(commands, want) {
		t.Errorf("hooks should list work before project, committed before local\ngot  %v\nwant %v", commands, want)
	}
	if got[2].Key != "hooks.0.args" || got[2].Value != "--strict" {
		t.Errorf("a hook's args should be listed, got %+v", got[2])
	}
}
