package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestShownLLMExtractTimeout(t *testing.T) {
	cases := []struct {
		name       string
		llm        model.LLMConfig
		wantValue  string
		wantSource model.ConfigValueSource
	}{
		{name: "unset", wantValue: model.DefaultLLMExtractTimeout.String(), wantSource: model.ConfigSourceDefault},
		{name: "set value wins", llm: model.LLMConfig{ExtractTimeout: "90s"}, wantValue: "90s", wantSource: model.ConfigSourceGlobal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, value := range model.EffectiveConfigValues(model.BaseConfig{LLM: tc.llm}, nil, nil) {
				if value.Key != "llm.extract_timeout" {
					continue
				}
				if value.Value != tc.wantValue || value.Source != tc.wantSource {
					t.Errorf("the shown llm.extract_timeout should be the one an extraction call runs under, got %+v", value)
				}
				return
			}
			t.Error("the effective config should list llm.extract_timeout")
		})
	}
}
