package model_test

import (
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/networkteam/sdd/internal/model"
)

// `sdd config` shows the defaults a run uses for the verify block.
func TestEffectiveConfigValues_ShowVerifyDefaults(t *testing.T) {
	values := verifyValuesByKey(model.EffectiveConfigValues(model.BaseConfig{}, nil, nil))
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
	if got := verifyValuesByKey(model.EffectiveConfigValues(model.BaseConfig{}, project, nil))["verify.prefilter"]; got.Value != "false" || got.Source != model.ConfigSourceProject {
		t.Fatalf("a prefilter switched off should show false from the project, got %+v", got)
	}
}

func verifyValuesByKey(values []model.ConfigValue) map[string]model.ConfigValue {
	byKey := make(map[string]model.ConfigValue, len(values))
	for _, v := range values {
		byKey[v.Key] = v
	}
	return byKey
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
// model; every value llm_prefilter sets wins. A dedicated provider takes the
// block as set.
func TestResolvedPrefilter(t *testing.T) {
	llm := model.LLMConfig{Provider: "openai", Model: "gpt-x", Timeout: "5m", Concurrency: 2, APIKeys: map[string]string{"openai": "k"}}

	derived := mustResolvePrefilter(t, model.BaseConfig{LLM: llm})
	if derived.Provider != "openai" || derived.Model != "gpt-x" {
		t.Errorf("no block should derive the llm provider and model, got %+v", derived)
	}
	if derived.Timeout != "" || derived.Concurrency != 0 || derived.APIKeys["openai"] != "k" {
		t.Errorf("the derived block should keep the prefilter's own bounds and the llm connection, got %+v", derived)
	}

	set := mustResolvePrefilter(t, model.BaseConfig{LLM: llm, LLMPrefilter: model.LLMPrefilterConfig{Model: "gpt-mini", Timeout: "45s"}})
	if set.Provider != "openai" || set.Model != "gpt-mini" || set.Timeout != "45s" {
		t.Errorf("values llm_prefilter sets should win, got %+v", set)
	}
	if derived.Protocol != model.PrefilterChat || set.Protocol != model.PrefilterChat {
		t.Errorf("a block without a protocol should run chat, got %q and %q", derived.Protocol, set.Protocol)
	}

	for _, provider := range []model.PrefilterProvider{model.PrefilterSystemOne, model.PrefilterCohere} {
		block := model.LLMPrefilterConfig{Provider: provider, APIKeys: map[string]string{string(provider): "p"}}
		dedicated := mustResolvePrefilter(t, model.BaseConfig{LLM: llm, LLMPrefilter: block})
		if dedicated.Model != "" || dedicated.Endpoint != "" || dedicated.APIKeys["openai"] != "" || dedicated.APIKeys[string(provider)] != "p" {
			t.Errorf("%s should take the block as set, nothing from llm, got %+v", provider, dedicated)
		}
	}
}

// A dedicated judge is named by llm_prefilter.provider only: from
// llm.provider it would run on the llm block's key, endpoint and model.
func TestResolvedPrefilter_RefusesADedicatedJudgeFromTheLLMBlock(t *testing.T) {
	for _, llm := range []model.LLMConfig{
		{Provider: "systemone", Model: "haiku", APIKeys: map[string]string{"systemone": "k"}},
		{Provider: "cohere", Model: "haiku", APIKeys: map[string]string{"cohere": "k"}},
	} {
		_, err := model.BaseConfig{LLM: llm}.ResolvedPrefilter()
		if err == nil || !strings.Contains(err.Error(), "llm_prefilter.provider") {
			t.Errorf("llm.provider %s should be refused, pointing at llm_prefilter.provider, got %v", llm.Provider, err)
		}
	}
}

func mustResolvePrefilter(t *testing.T, b model.BaseConfig) model.LLMPrefilterConfig {
	t.Helper()
	resolved, err := b.ResolvedPrefilter()
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// llm_prefilter.provider takes every chat provider the llm block knows, then
// the dedicated judges.
func TestPrefilterProvider_AllowsEveryChatProviderAndTheDedicatedJudges(t *testing.T) {
	allowed := model.PrefilterProvider("").AllowedValues()
	for _, provider := range append(model.LLMProviders(), "systemone", "cohere") {
		if !slices.Contains(allowed, provider) {
			t.Errorf("llm_prefilter.provider should allow %s, allows %v", provider, allowed)
		}
	}
}

// The block holds its provider in a field of its own, beside the llm keys.
func TestParseConfig_ReadsTheLLMPrefilterBlock(t *testing.T) {
	cfg, err := model.ParseConfig([]byte("llm_prefilter:\n  provider: cohere\n  model: m\n  endpoint: https://example.invalid/rerank\n  timeout: 10s\n  api_keys:\n    cohere: k\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.LLMPrefilter
	if got.Provider != model.PrefilterCohere || got.Model != "m" || got.Endpoint != "https://example.invalid/rerank" || got.Timeout != "10s" || got.APIKeys["cohere"] != "k" {
		t.Fatalf("the block should read every key it sets, got %+v", got)
	}
}

// A protocol name a former sdd wrote reads as the protocol it became.
func TestPrefilterProtocol_ReadsLegacyNamesAsChat(t *testing.T) {
	for _, name := range []string{"chat", "generate", "listwise"} {
		t.Run(name, func(t *testing.T) {
			var block model.LLMPrefilterConfig
			if err := yaml.Unmarshal([]byte("protocol: "+name+"\nmodel: haiku\n"), &block); err != nil {
				t.Fatal(err)
			}
			if block.Protocol != model.PrefilterChat || block.Model != "haiku" {
				t.Fatalf("block %+v: the name should read as chat beside the model", block)
			}
		})
	}
}
