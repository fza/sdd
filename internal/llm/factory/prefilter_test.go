package factory_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

func TestNewPrefilter(t *testing.T) {
	keys := map[string]string{"systemone": "k", "cohere": "k"}
	cases := []struct {
		name      string
		cfg       model.LLMPrefilterConfig
		judge     string
		direction relevance.Direction
		batch     int
		pair      int
	}{
		{"chat provider runs generate", model.LLMPrefilterConfig{Provider: "claude-cli", Model: "haiku"}, "generate:haiku", relevance.Both, 1, 0},
		{"systemone defaults to Jev through Opper", model.LLMPrefilterConfig{Provider: "systemone", APIKeys: keys}, "systemone:jev-1.14.0", relevance.Both, 0, 0},
		{"cohere defaults to rerank v3.5 through Opper", model.LLMPrefilterConfig{Provider: "cohere", APIKeys: keys}, "cohere:aws/cohere/rerank-v3.5", relevance.Forward, 1000, 4096},
		{"a set model names the judge", model.LLMPrefilterConfig{Provider: "cohere", Model: "cohere/rerank-v4", APIKeys: keys}, "cohere:cohere/rerank-v4", relevance.Forward, 1000, 4096},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prefilter, err := factory.NewPrefilter(tc.cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			scorer := prefilter.Scorer
			if prefilter.Judge != tc.judge || scorer.Direction() != tc.direction || scorer.MaxBatch() != tc.batch || scorer.MaxPairTokens() != tc.pair {
				t.Fatalf("judge %q: the provider should pick its judge, direction, batch and pair window", prefilter.Judge)
			}
		})
	}
}

func TestNewPrefilter_RefusesABadBlock(t *testing.T) {
	cases := []struct {
		name string
		cfg  model.LLMPrefilterConfig
		want string
	}{
		{"systemone without its key", model.LLMPrefilterConfig{Provider: "systemone", APIKeys: map[string]string{"opper": "k"}}, "llm_prefilter: provider systemone needs llm_prefilter.api_keys.systemone"},
		{"cohere without its key", model.LLMPrefilterConfig{Provider: "cohere"}, "llm_prefilter: provider cohere needs llm_prefilter.api_keys.cohere"},
		{"relative endpoint", model.LLMPrefilterConfig{Provider: "cohere", Endpoint: "/v3/rerank", APIKeys: map[string]string{"cohere": "k"}}, "not an absolute URL"},
		{"unknown provider", model.LLMPrefilterConfig{Provider: "bm25"}, "unknown llm provider \"bm25\""},
		{"bad timeout", model.LLMPrefilterConfig{Provider: "claude-cli", Timeout: "soon"}, "parsing timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := factory.NewPrefilter(tc.cfg, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("NewPrefilter = %v, should name the problem", err)
			}
		})
	}
}

// Without a model of its own the judge is named after the default model.
func TestNewPrefilter_DefaultModelNamesTheJudge(t *testing.T) {
	generate, err := factory.NewPrefilter(model.LLMPrefilterConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if generate.Judge != "generate:"+model.DefaultLLMModel {
		t.Fatalf("judge %q should name the default model", generate.Judge)
	}
}
