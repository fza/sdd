package factory_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm/relevance"
	"github.com/networkteam/sdd/pkg/llm/relevance/cohere"
	"github.com/networkteam/sdd/pkg/llm/relevance/systemone"
)

func TestNewPrefilter(t *testing.T) {
	keys := map[string]string{"systemone": "k", "cohere": "k"}
	jev, err := systemone.New(systemone.Options{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	rerank, err := cohere.New(cohere.Options{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		cfg       model.LLMPrefilterConfig
		judge     string
		direction relevance.Direction
		batch     int
		pair      int
	}{
		{"chat provider runs the chat judge", model.LLMPrefilterConfig{Protocol: model.PrefilterChat, Provider: "claude-cli", Model: "haiku"}, "chat:haiku", relevance.Both, llmops.ChatBatch, 0},
		{"systemone defaults to Jev through Opper", model.LLMPrefilterConfig{Provider: "systemone", APIKeys: keys}, "systemone:jev-1.14.0", relevance.Both, jev.MaxBatch(), 0},
		{"cohere defaults to rerank v3.5 through Opper", model.LLMPrefilterConfig{Provider: "cohere", APIKeys: keys}, "cohere:aws/cohere/rerank-v3.5", relevance.Forward, 1000, rerank.MaxPairTokens()},
		{"a set model names the judge", model.LLMPrefilterConfig{Provider: "cohere", Model: "cohere/rerank-v4", APIKeys: keys}, "cohere:cohere/rerank-v4", relevance.Forward, 1000, rerank.MaxPairTokens()},
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
		{"relative endpoint", model.LLMPrefilterConfig{Provider: "cohere", Endpoint: "/v3/rerank", APIKeys: map[string]string{"cohere": "k"}}, "not an absolute http or https URL"},
		{"unknown provider", model.LLMPrefilterConfig{Protocol: model.PrefilterChat, Provider: "bm25"}, "unknown llm provider \"bm25\""},
		{"params on systemone", model.LLMPrefilterConfig{Provider: "systemone", Params: map[string]string{"temperature": "0"}, APIKeys: map[string]string{"systemone": "k"}}, "provider systemone takes no params"},
		{"concurrency and retry on cohere", model.LLMPrefilterConfig{Provider: "cohere", Concurrency: 4, Retry: model.RetryConfig{MaxAttempts: 2}, APIKeys: map[string]string{"cohere": "k"}}, "provider cohere takes no concurrency, retry"},
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
	resolved, err := model.BaseConfig{}.ResolvedPrefilter()
	if err != nil {
		t.Fatal(err)
	}
	chat, err := factory.NewPrefilter(resolved, nil)
	if err != nil {
		t.Fatal(err)
	}
	if relevance.Reads(chat.Scorer, relevance.Candidate{ID: "a", Text: "kind: gap\n\nA body."}) {
		t.Fatal("the composed chat judge should read no candidate without a line")
	}
	if chat.Judge != "chat:"+model.DefaultLLMModel {
		t.Fatalf("judge %q should name the default model", chat.Judge)
	}
}

// A block without a protocol or with an unknown one is refused, naming the
// protocols there are.
func TestNewPrefilter_RefusesAMissingOrUnknownProtocol(t *testing.T) {
	for name, protocol := range map[string]model.PrefilterProtocol{"missing": "", "unknown": "listwise-v2"} {
		t.Run(name, func(t *testing.T) {
			_, err := factory.NewPrefilter(model.LLMPrefilterConfig{Protocol: protocol}, nil)
			if err == nil || !strings.Contains(err.Error(), "llm_prefilter.protocol") || !strings.Contains(err.Error(), "chat") {
				t.Fatalf("NewPrefilter = %v, should name the protocol key and the protocols there are", err)
			}
		})
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
	prefilter, err := factory.NewPrefilter(model.LLMPrefilterConfig{Provider: "cohere", Endpoint: server.URL, APIKeys: map[string]string{"cohere": "k"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = prefilter.Scorer.Score(t.Context(), relevance.Request{Subject: "draft", Candidates: []relevance.Candidate{{ID: "a", Text: "candidate"}}})
	if err != nil || calls != 2 {
		t.Fatalf("%d calls, error %v: the judge should call once more after the delay", calls, err)
	}
}
