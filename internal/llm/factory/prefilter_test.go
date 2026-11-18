package factory_test

import (
	"io"
	"net/http"
	"net/http/httptest"
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
	rerank, err := factory.NewPrefilter(model.LLMPrefilterConfig{Protocol: "cohere-rerank", LLMConfig: model.LLMConfig{APIKeys: map[string]string{"opper": "k"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rerank.Judge != "cohere-rerank:aws/cohere/rerank-v3.5" || rerank.Scorer.Direction() != relevance.Forward || rerank.Scorer.MaxPairTokens() == 0 {
		t.Fatalf("judge %q: cohere-rerank should default to rerank v3.5 through Opper, one direction, each pair in its window", rerank.Judge)
	}
	generate, err := factory.NewPrefilter(model.LLMPrefilterConfig{Protocol: "generate", LLMConfig: model.LLMConfig{Provider: "claude-cli", Model: "haiku"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if generate.Judge != "generate:haiku" || generate.Scorer.MaxBatch() != 1 {
		t.Fatalf("judge %q: the generate bridge should rate each pair in a call of its own", generate.Judge)
	}
	listwise, err := factory.NewPrefilter(model.LLMPrefilterConfig{Protocol: "listwise", LLMConfig: model.LLMConfig{Provider: "claude-cli", Model: "claude-haiku-5-5", Effort: "low"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if listwise.Judge != "listwise:claude-haiku-5-5" || listwise.Scorer.MaxBatch() != 100 || listwise.Scorer.Direction() != relevance.Both {
		t.Fatalf("judge %q: listwise should score up to 100 candidates a call", listwise.Judge)
	}
	for name, tc := range map[string]struct {
		cfg  model.LLMPrefilterConfig
		want string
	}{
		"no protocol":      {model.LLMPrefilterConfig{}, "llm_prefilter.protocol is not set"},
		"unknown protocol": {model.LLMPrefilterConfig{Protocol: "magic"}, "unknown llm_prefilter.protocol"},
		"no key":           {model.LLMPrefilterConfig{Protocol: "systemone"}, "llm_prefilter.api_keys.opper"},
		"no rerank key":    {model.LLMPrefilterConfig{Protocol: "cohere-rerank", LLMConfig: model.LLMConfig{Provider: "cohere"}}, "protocol cohere-rerank needs llm_prefilter.api_keys.cohere"},
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

// A generate prefilter on codex's own default model judges as the provider.
func TestNewPrefilter_GenerateOnCodexDefaultModel(t *testing.T) {
	generate, err := factory.NewPrefilter(model.LLMPrefilterConfig{Protocol: "generate", LLMConfig: model.LLMConfig{Provider: "codex", Effort: "low"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if generate.Judge != "generate:codex" {
		t.Fatalf("judge %q: an empty codex model should judge as the provider, never as a Claude model", generate.Judge)
	}
}

// A dedicated judge asked to wait by a 429 calls once more within its timeout.
func TestNewPrefilter_RetriesAfterTheDelayA429AsksFor(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"results":[{"index":0,"relevance_score":0.5}]}`)
	}))
	t.Cleanup(server.Close)
	prefilter, err := factory.NewPrefilter(model.LLMPrefilterConfig{Protocol: model.PrefilterCohereRerank, LLMConfig: model.LLMConfig{Endpoint: server.URL, APIKeys: map[string]string{"opper": "k"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = prefilter.Scorer.Score(t.Context(), relevance.Request{Subject: "draft", Candidates: []relevance.Candidate{{ID: "a", Text: "candidate"}}})
	if err != nil || calls != 2 {
		t.Fatalf("%d calls, error %v: the judge should call once more after the delay", calls, err)
	}
}
