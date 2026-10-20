package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// A zero cap is a setting, not an absence: it must override a cap set below.
func TestMergeConfigMaxTokensZeroOverrides(t *testing.T) {
	baseCap, overlayCap := 32768, 0
	base := &model.PerRepoConfig{}
	base.LLM.MaxTokens = &baseCap
	overlay := &model.PerRepoConfig{}
	overlay.LLM.MaxTokens = &overlayCap

	merged := model.MergeConfig(base, overlay)

	got, err := merged.LLM.ResolvedMaxTokens()
	if err != nil {
		t.Fatalf("ResolvedMaxTokens: %v", err)
	}
	if got != 0 {
		t.Errorf("the overlay's zero cap should win over the base's, got %d", got)
	}
	unset, err := model.MergeConfig(base, &model.PerRepoConfig{}).LLM.ResolvedMaxTokens()
	if err != nil {
		t.Fatalf("ResolvedMaxTokens: %v", err)
	}
	if unset != baseCap {
		t.Errorf("an overlay leaving the key unset should keep the base's cap, got %d", unset)
	}
}

// A zero cap is set by the layer that wrote it and renders as the number, not
// as the pointer holding it.
func TestEffectiveConfigValuesRendersAZeroMaxTokens(t *testing.T) {
	zero := 0
	local := &model.PerRepoConfig{}
	local.LLM.MaxTokens = &zero

	for _, v := range model.EffectiveConfigValues(model.BaseConfig{}, nil, local) {
		if v.Key != "llm.max_tokens" {
			continue
		}
		if v.Value != "0" || v.Source != model.ConfigSourceLocal {
			t.Errorf("a zero cap should render as set by its layer, got %+v", v)
		}
		return
	}
	t.Fatal("llm.max_tokens should be an effective config key")
}

func TestEffectiveConfigValuesShowsTheMaxTokensDefault(t *testing.T) {
	for _, v := range model.EffectiveConfigValues(model.BaseConfig{}, nil, nil) {
		if v.Key == "llm.max_tokens" {
			if v.Source != model.ConfigSourceDefault || v.Value != "4096" {
				t.Errorf("an unset cap should show the baked default, got %+v", v)
			}
			return
		}
	}
	t.Fatal("llm.max_tokens should be an effective config key")
}
