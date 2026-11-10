package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func debugLayer(on bool) *model.PerRepoConfig {
	return &model.PerRepoConfig{BaseConfig: model.BaseConfig{LLM: model.LLMConfig{Debug: &on}}}
}

func TestLLMDebugLoadsFromConfig(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want bool
	}{
		{name: "unset", yaml: "llm:\n  model: small\n"},
		{name: "on", yaml: "llm:\n  debug: true\n", want: true},
		{name: "off", yaml: "llm:\n  debug: false\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := model.ParseConfig([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("the key should load: %v", err)
			}
			if got := cfg.LLM.DebugOn(); got != tc.want {
				t.Errorf("transcripts should be on only when the key says true, got %v", got)
			}
		})
	}
}

func TestLLMDebugLayering(t *testing.T) {
	cases := []struct {
		name    string
		base    *model.PerRepoConfig
		overlay *model.PerRepoConfig
		want    bool
	}{
		{name: "local turns it on", base: &model.PerRepoConfig{}, overlay: debugLayer(true), want: true},
		{name: "local turns a global on off", base: debugLayer(true), overlay: debugLayer(false)},
		{name: "unset local keeps global", base: debugLayer(true), overlay: &model.PerRepoConfig{}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.MergeConfig(tc.base, tc.overlay).LLM.DebugOn(); got != tc.want {
				t.Errorf("the highest layer setting the key should decide, got %v", got)
			}
		})
	}
}

func TestHonoredCommittedDropsLLMDebug(t *testing.T) {
	committed := debugLayer(true)
	committed.GraphDir = "graph"
	honored := model.HonoredCommitted(committed)
	if honored.LLM.Debug != nil || honored.GraphDir != "graph" {
		t.Errorf("only llm.debug should be dropped from the committed layer, got %+v", honored)
	}
	if !committed.LLM.DebugOn() {
		t.Error("the layer read should be left as it was")
	}
}

func TestIgnoredLLMDebug(t *testing.T) {
	on := true
	layer := model.BaseConfig{LLM: model.LLMConfig{Debug: &on}}
	got := model.IgnoredSettings(layer, model.ConfigSourceProject)
	if len(got) != 1 || got[0].Key != "llm.debug" || got[0].Reason == "" {
		t.Errorf("llm.debug in the committed config should be reported as ignored with a reason, got %v", got)
	}
	for _, source := range []model.ConfigValueSource{model.ConfigSourceLocal, model.ConfigSourceGlobal} {
		if got := model.IgnoredSettings(layer, source); len(got) != 0 {
			t.Errorf("a person's own config may set llm.debug, got %v for %s", got, source)
		}
	}
}

func TestLLMDebugEffectiveDefault(t *testing.T) {
	for _, v := range model.EffectiveConfigValues(model.BaseConfig{}, nil, nil) {
		if v.Key == "llm.debug" {
			if v.Value != "false" {
				t.Errorf("an unset llm.debug should show as off, got %q", v.Value)
			}
			return
		}
	}
	t.Error("the effective config should list llm.debug")
}
