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

// `sdd config` shows the defaults a run uses for the verify block and the
// llm effort.
func TestEffectiveConfigValues_ShowVerifyAndEffortDefaults(t *testing.T) {
	values := effective(model.EffectiveConfigValues(model.BaseConfig{}, nil, nil))
	for key, want := range map[string]string{
		"verify.mode":          "prose",
		"verify.prefilter":     "true",
		"verify.runner":        "subagent",
		"verify.claude.model":  "sonnet",
		"verify.claude.effort": "medium",
		"verify.codex.effort":  "medium",
		"llm.effort":           "medium",
	} {
		if got := values[key]; got.Value != want || got.Source != model.ConfigSourceDefault {
			t.Errorf("%s = %+v, want the default %s", key, got, want)
		}
	}
	if got, shown := values["verify.codex.model"]; shown {
		t.Errorf("verify.codex.model leaves codex's own default and shows none, got %+v", got)
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

// The llm defaults follow the provider: codex runs its own default model, and
// only the CLI providers take an effort.
func TestEffectiveConfigValues_LLMDefaultsFollowTheProvider(t *testing.T) {
	codex := effective(model.EffectiveConfigValues(model.BaseConfig{LLM: model.LLMConfig{Provider: "codex"}}, nil, nil))
	if got, shown := codex["llm.model"]; shown {
		t.Errorf("codex should show no default llm.model, got %+v", got)
	}
	if got := codex["llm.effort"]; got.Value != "medium" {
		t.Errorf("codex should show the default effort, got %+v", got)
	}
	anthropic := effective(model.EffectiveConfigValues(model.BaseConfig{LLM: model.LLMConfig{Provider: "anthropic"}}, nil, nil))
	if got, shown := anthropic["llm.effort"]; shown {
		t.Errorf("an API provider takes no llm.effort, got %+v", got)
	}
	if got := anthropic["llm.model"]; got.Value != model.DefaultLLMModel {
		t.Errorf("anthropic should show the default model, got %+v", got)
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

func TestLLMConfig_ResolvedModel(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg  model.LLMConfig
		want string
	}{
		"default provider":   {model.LLMConfig{}, model.DefaultLLMModel},
		"codex leaves empty": {model.LLMConfig{Provider: "codex"}, ""},
		"set model wins":     {model.LLMConfig{Provider: "codex", Model: "gpt-5.5"}, "gpt-5.5"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.cfg.ResolvedModel(); got != tc.want {
				t.Fatalf("ResolvedModel = %q, want %q", got, tc.want)
			}
		})
	}
}

// Without an llm_prefilter block the prefilter runs generate on the llm
// provider and model at effort low; every value llm_prefilter sets wins.
func TestResolvedPrefilter(t *testing.T) {
	llm := model.LLMConfig{Provider: "codex", Model: "gpt-5.5", Effort: "high", Timeout: "5m", Concurrency: 2, APIKeys: map[string]string{"openai": "k"}}

	derived := model.BaseConfig{LLM: llm}.ResolvedPrefilter()
	if derived.Protocol != model.PrefilterGenerate || derived.Provider != "codex" || derived.Model != "gpt-5.5" || derived.Effort != "low" {
		t.Errorf("no block should derive generate on the llm provider and model at low, got %+v", derived)
	}
	if derived.Timeout != "" || derived.Concurrency != 0 || derived.APIKeys["openai"] != "k" {
		t.Errorf("the derived block should keep the prefilter's own bounds and the llm connection, got %+v", derived)
	}

	set := model.BaseConfig{LLM: llm, LLMPrefilter: model.LLMPrefilterConfig{LLMConfig: model.LLMConfig{Model: "gpt-mini", Effort: "minimal"}}}.ResolvedPrefilter()
	if set.Protocol != model.PrefilterGenerate || set.Provider != "codex" || set.Model != "gpt-mini" || set.Effort != "minimal" {
		t.Errorf("values llm_prefilter sets should win, got %+v", set)
	}

	rate := model.BaseConfig{LLM: llm, LLMPrefilter: model.LLMPrefilterConfig{Protocol: model.PrefilterSystemOne}}.ResolvedPrefilter()
	if rate.Provider != "" || rate.Model != "" || rate.Effort != "" {
		t.Errorf("a rating protocol should take the block as set, got %+v", rate)
	}
}
