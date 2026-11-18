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
	systemone, err := factory.NewPrefilter(model.LLMPrefilterConfig{Protocol: "systemone", LLMConfig: model.LLMConfig{APIKeys: map[string]string{"opper": "k"}}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if systemone.Judge != "systemone:jev-1.14.0" || systemone.Scorer.Direction() != relevance.Both {
		t.Fatalf("judge %q: systemone should default to Jev", systemone.Judge)
	}
	rerank, err := factory.NewPrefilter(model.LLMPrefilterConfig{Protocol: "cohere", LLMConfig: model.LLMConfig{APIKeys: map[string]string{"opper": "k"}}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rerank.Judge != "cohere:aws/cohere/rerank-v3.5" || rerank.Scorer.Direction() != relevance.Forward || rerank.Scorer.MaxPairTokens() == 0 {
		t.Fatalf("judge %q: cohere should default to rerank v3.5 through Opper, one direction, each pair in its window", rerank.Judge)
	}
	chat, err := factory.NewPrefilter(model.LLMPrefilterConfig{Protocol: "chat", LLMConfig: model.LLMConfig{Provider: "claude-cli", Model: "claude-haiku-5-5", Effort: "low"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if chat.Judge != "chat:claude-haiku-5-5" || chat.Scorer.MaxBatch() != 100 || chat.Scorer.Direction() != relevance.Both {
		t.Fatalf("judge %q: chat should start at 100 candidates a call", chat.Judge)
	}
	sizes := &relevance.MemoryBatchSizes{}
	if err := sizes.Lower("chat:claude-haiku-5-5", 25); err != nil {
		t.Fatal(err)
	}
	remembered, err := factory.NewPrefilter(model.LLMPrefilterConfig{Protocol: "chat", LLMConfig: model.LLMConfig{Provider: "claude-cli", Model: "claude-haiku-5-5"}}, nil, sizes)
	if err != nil {
		t.Fatal(err)
	}
	if remembered.Scorer.MaxBatch() != 25 {
		t.Fatalf("MaxBatch = %d: chat should start at the size remembered for its judge", remembered.Scorer.MaxBatch())
	}
	for name, tc := range map[string]struct {
		cfg  model.LLMPrefilterConfig
		want string
	}{
		"no protocol":      {model.LLMPrefilterConfig{}, "llm_prefilter.protocol is not set"},
		"unknown protocol": {model.LLMPrefilterConfig{Protocol: "magic"}, "unknown llm_prefilter.protocol"},
		"no key":           {model.LLMPrefilterConfig{Protocol: "systemone"}, "llm_prefilter.api_keys.opper"},
		"no rerank key":    {model.LLMPrefilterConfig{Protocol: "cohere", LLMConfig: model.LLMConfig{Provider: "cohere"}}, "protocol cohere needs llm_prefilter.api_keys.cohere"},
		"bad timeout":      {model.LLMPrefilterConfig{Protocol: "systemone", LLMConfig: model.LLMConfig{Timeout: "soon"}}, "parsing timeout"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := factory.NewPrefilter(tc.cfg, nil, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("NewPrefilter = %v, should name the problem", err)
			}
		})
	}
}

// A chat prefilter on codex's own default model judges as the provider.
func TestNewPrefilter_ChatOnCodexDefaultModel(t *testing.T) {
	chat, err := factory.NewPrefilter(model.LLMPrefilterConfig{Protocol: "chat", LLMConfig: model.LLMConfig{Provider: "codex", Effort: "low"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if chat.Judge != "chat:codex" {
		t.Fatalf("judge %q: an empty codex model should judge as the provider, never as a Claude model", chat.Judge)
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
	prefilter, err := factory.NewPrefilter(model.LLMPrefilterConfig{Protocol: model.PrefilterCohere, LLMConfig: model.LLMConfig{Endpoint: server.URL, APIKeys: map[string]string{"opper": "k"}}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = prefilter.Scorer.Score(t.Context(), relevance.Request{Subject: "draft", Candidates: []relevance.Candidate{{ID: "a", Text: "candidate"}}})
	if err != nil || calls != 2 {
		t.Fatalf("%d calls, error %v: the judge should call once more after the delay", calls, err)
	}
}
