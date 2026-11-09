package factory_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

func TestNewPrefilter(t *testing.T) {
	systemone, err := factory.NewPrefilter(model.LLMPrefilterConfig{Protocol: "systemone", LLMConfig: model.LLMConfig{APIKeys: map[string]string{"opper": "k"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if systemone.Judge != "systemone:jev-1.14.0" || systemone.Scorer.Direction() != relevance.Both {
		t.Fatalf("judge %q: systemone should default to Jev", systemone.Judge)
	}
	generate, err := factory.NewPrefilter(model.LLMPrefilterConfig{Protocol: "generate", LLMConfig: model.LLMConfig{Provider: "claude-cli", Model: "haiku"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if generate.Judge != "generate:haiku" || generate.Scorer.MaxBatch() != 0 {
		t.Fatalf("judge %q: the generate bridge should fan a batch out per pair", generate.Judge)
	}
	for name, tc := range map[string]struct {
		cfg  model.LLMPrefilterConfig
		want string
	}{
		"no protocol":      {model.LLMPrefilterConfig{}, "llm_prefilter.protocol is not set"},
		"unknown protocol": {model.LLMPrefilterConfig{Protocol: "magic"}, "unknown llm_prefilter.protocol"},
		"no key":           {model.LLMPrefilterConfig{Protocol: "systemone"}, "llm_prefilter.api_keys.opper"},
		"bad timeout":      {model.LLMPrefilterConfig{Protocol: "systemone", LLMConfig: model.LLMConfig{Timeout: "soon"}}, "parsing timeout"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := factory.NewPrefilter(tc.cfg, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("NewPrefilter = %v, should name the problem", err)
			}
		})
	}
}
