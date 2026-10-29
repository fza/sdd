package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestShownLLMTimeoutFollowsTheProvider(t *testing.T) {
	cases := []struct {
		name       string
		llm        model.LLMConfig
		wantValue  string
		wantSource model.ConfigValueSource
	}{
		{name: "default provider", wantValue: model.DefaultClaudeCLITimeout, wantSource: model.ConfigSourceDefault},
		{name: "claude-cli named", llm: model.LLMConfig{Provider: "claude-cli"}, wantValue: model.DefaultClaudeCLITimeout, wantSource: model.ConfigSourceDefault},
		{name: "api provider", llm: model.LLMConfig{Provider: "anthropic"}, wantValue: model.DefaultLLMTimeout, wantSource: model.ConfigSourceDefault},
		{name: "set value wins", llm: model.LLMConfig{Timeout: "7m"}, wantValue: "7m", wantSource: model.ConfigSourceGlobal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, value := range model.EffectiveConfigValues(model.BaseConfig{LLM: tc.llm}, nil, nil) {
				if value.Key != "llm.timeout" {
					continue
				}
				if value.Value != tc.wantValue || value.Source != tc.wantSource {
					t.Errorf("the shown llm.timeout should be the one a call runs under, got %+v", value)
				}
				return
			}
			t.Error("the effective config should list llm.timeout")
		})
	}
}
