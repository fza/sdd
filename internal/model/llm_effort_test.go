package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestResolvedEffortDefaultsPerProvider(t *testing.T) {
	for provider, want := range map[string]string{"": "medium", "claude-cli": "medium", "anthropic": "medium", "openai": "low", "ollama": "low", "unknown": ""} {
		if got := (model.LLMConfig{Provider: provider}).ResolvedEffort(); got != want {
			t.Errorf("ResolvedEffort for %q = %q, want %q", provider, got, want)
		}
	}
	if got := (model.LLMConfig{Provider: "openai", Effort: "high"}).ResolvedEffort(); got != "high" {
		t.Errorf("a set llm.effort should win, got %q", got)
	}
}

// sdd config shows the default effort of the effective provider, and a set
// llm.effort with its layer.
func TestEffectiveConfigShowsTheEffortOfTheProvider(t *testing.T) {
	effort := func(global model.BaseConfig, project *model.PerRepoConfig) (model.ConfigValue, bool) {
		for _, v := range model.EffectiveConfigValues(global, project, nil) {
			if v.Key == "llm.effort" {
				return v, true
			}
		}
		return model.ConfigValue{}, false
	}
	for provider, want := range map[string]string{"": "medium", "anthropic": "medium", "openai": "low"} {
		got, shown := effort(model.BaseConfig{LLM: model.LLMConfig{Provider: provider}}, nil)
		if !shown || got.Value != want || got.Source != model.ConfigSourceDefault {
			t.Errorf("provider %q: llm.effort = %+v (shown %v), want default %s", provider, got, shown, want)
		}
	}
	project := &model.PerRepoConfig{BaseConfig: model.BaseConfig{LLM: model.LLMConfig{Provider: "ollama"}}}
	if got, _ := effort(model.BaseConfig{LLM: model.LLMConfig{Provider: "anthropic"}}, project); got.Value != "low" {
		t.Errorf("the project's provider should decide the default, got %+v", got)
	}
	if got, shown := effort(model.BaseConfig{LLM: model.LLMConfig{Provider: "unknown"}}, nil); shown {
		t.Errorf("an unknown provider should show no default effort, got %+v", got)
	}
	set := model.BaseConfig{LLM: model.LLMConfig{Provider: "openai", Effort: "high"}}
	if got, _ := effort(set, nil); got.Value != "high" || got.Source != model.ConfigSourceGlobal {
		t.Errorf("a set llm.effort should show with its layer, got %+v", got)
	}
}
