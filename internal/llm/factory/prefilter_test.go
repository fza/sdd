package factory_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

func TestNewPrefilter(t *testing.T) {
	generate, err := factory.NewPrefilter(model.LLMPrefilterConfig{LLMConfig: model.LLMConfig{Provider: "claude-cli", Model: "haiku"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if generate.Judge != "generate:haiku" || generate.Scorer.MaxBatch() != 1 || generate.Scorer.Direction() != relevance.Both {
		t.Fatalf("judge %q: the generate bridge should read both texts and score one pair per call", generate.Judge)
	}
	_, err = factory.NewPrefilter(model.LLMPrefilterConfig{LLMConfig: model.LLMConfig{Provider: "claude-cli", Timeout: "soon"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "parsing timeout") {
		t.Fatalf("NewPrefilter = %v, should name the malformed timeout", err)
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
