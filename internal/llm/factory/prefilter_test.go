package factory_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

func TestNewPrefilter(t *testing.T) {
	chat, err := factory.NewPrefilter(model.LLMPrefilterConfig{LLMConfig: model.LLMConfig{Provider: "claude-cli", Model: "haiku"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if chat.Judge != "chat:haiku" || chat.Scorer.MaxBatch() != llmops.ChatBatch || chat.Scorer.Direction() != relevance.Both {
		t.Fatalf("judge %q: the chat judge should read both texts and score a batch per call", chat.Judge)
	}
	if relevance.Reads(chat.Scorer, relevance.Candidate{ID: "a", Text: "kind: gap\n\nA body."}) {
		t.Fatal("the composed chat judge should read no candidate without a line")
	}
	_, err = factory.NewPrefilter(model.LLMPrefilterConfig{LLMConfig: model.LLMConfig{Provider: "claude-cli", Timeout: "soon"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "parsing timeout") {
		t.Fatalf("NewPrefilter = %v, should name the malformed timeout", err)
	}
}

// Without a model of its own the judge is named after the default model.
func TestNewPrefilter_DefaultModelNamesTheJudge(t *testing.T) {
	chat, err := factory.NewPrefilter(model.BaseConfig{}.ResolvedPrefilter(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if chat.Judge != "chat:"+model.DefaultLLMModel {
		t.Fatalf("judge %q should name the default model", chat.Judge)
	}
}
