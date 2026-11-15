package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func effective(values []model.ConfigValue) map[string]model.ConfigValue {
	byKey := make(map[string]model.ConfigValue, len(values))
	for _, v := range values {
		byKey[v.Key] = v
	}
	return byKey
}

// `sdd config` shows the defaults a run uses for the verify block.
func TestEffectiveConfigValues_ShowVerifyDefaults(t *testing.T) {
	values := effective(model.EffectiveConfigValues(model.BaseConfig{}, nil, nil))
	for key, want := range map[string]string{
		"verify.mode":          "prose",
		"verify.prefilter":     "true",
		"verify.runner":        "subagent",
		"verify.claude.model":  "sonnet",
		"verify.claude.effort": "medium",
	} {
		if got := values[key]; got.Value != want || got.Source != model.ConfigSourceDefault {
			t.Errorf("%s = %+v, want the default %s", key, got, want)
		}
	}
}

func TestEffectiveConfigValues_PrefilterSetOffShowsFalse(t *testing.T) {
	off := false
	project := &model.PerRepoConfig{}
	project.Verify.Prefilter = &off
	if got := effective(model.EffectiveConfigValues(model.BaseConfig{}, project, nil))["verify.prefilter"]; got.Value != "false" || got.Source != model.ConfigSourceProject {
		t.Fatalf("a prefilter switched off should show false from the project, got %+v", got)
	}
}

func TestGroundingPolicy_PrefilterOnByDefault(t *testing.T) {
	if !(model.VerifyConfig{}).GroundingPolicy().Prefilter {
		t.Fatal("an unset verify.prefilter should be on")
	}
	off := false
	if (model.VerifyConfig{Prefilter: &off}).GroundingPolicy().Prefilter {
		t.Fatal("verify.prefilter false should switch it off")
	}
}

// Without an llm_prefilter block the prefilter runs on the llm provider and
// model; every value llm_prefilter sets wins.
func TestResolvedPrefilter(t *testing.T) {
	llm := model.LLMConfig{Provider: "openai", Model: "gpt-x", Timeout: "5m", Concurrency: 2, APIKeys: map[string]string{"openai": "k"}}

	derived := model.BaseConfig{LLM: llm}.ResolvedPrefilter()
	if derived.Provider != "openai" || derived.Model != "gpt-x" {
		t.Errorf("no block should derive the llm provider and model, got %+v", derived)
	}
	if derived.Timeout != "" || derived.Concurrency != 0 || derived.APIKeys["openai"] != "k" {
		t.Errorf("the derived block should keep the prefilter's own bounds and the llm connection, got %+v", derived)
	}

	set := model.BaseConfig{LLM: llm, LLMPrefilter: model.LLMPrefilterConfig{LLMConfig: model.LLMConfig{Model: "gpt-mini", Timeout: "45s"}}}.ResolvedPrefilter()
	if set.Provider != "openai" || set.Model != "gpt-mini" || set.Timeout != "45s" {
		t.Errorf("values llm_prefilter sets should win, got %+v", set)
	}
}
