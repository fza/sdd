package gollm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"

	gollmrunner "github.com/networkteam/sdd/internal/llm/gollm"
)

// effortSent runs one request through a runner for cfg against a local
// listener and returns the request body as the provider received it.
func effortSent(t *testing.T, cfg model.LLMConfig, schema bool) map[string]any {
	t.Helper()
	var mu sync.Mutex
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_, _ = w.Write([]byte(`{"models":[]}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		raw = body
		mu.Unlock()
		answer := jsonString(`{"reasoning":"r","axis":"form"}`)
		switch cfg.Provider {
		case "openai":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + answer + `}}]}`))
		case "anthropic":
			_, _ = w.Write([]byte(`{"content":[{"type":"text","text":` + answer + `}]}`))
		default:
			_, _ = w.Write([]byte(`{"model":"m","response":` + answer + `,"done":true}`))
		}
	}))
	defer srv.Close()
	switch cfg.Provider {
	case "ollama":
		cfg.OllamaEndpoint = srv.URL
	default:
		cfg.Endpoint = srv.URL
		cfg.APIKeys = map[string]string{cfg.Provider: "op-notarealkeyaaaaaaaaaa"}
	}
	runner, err := gollmrunner.NewRunner(cfg)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	req := llm.Request{SystemPrompt: "S", UserPrompt: "U"}
	if schema {
		req.ResponseSchema = json.RawMessage(wireSchema)
	}
	if _, err := runner.Run(context.Background(), req); err != nil {
		t.Fatalf("Run: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("request body %s: %v", raw, err)
	}
	return body
}

func anthropicEffort(body map[string]any) any {
	config, _ := body["output_config"].(map[string]any)
	return config["effort"]
}

// The effort reaches each API provider in its own parameter, at the
// provider's default when llm.effort is unset, and never reaches a model
// whose API refuses the parameter.
func TestEffortReachesTheModelsThatTakeIt(t *testing.T) {
	cases := []struct {
		name string
		cfg  model.LLMConfig
		sent func(map[string]any) any
		want any
	}{
		{"anthropic default", model.LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4-6"}, anthropicEffort, "medium"},
		{"anthropic set", model.LLMConfig{Provider: "anthropic", Model: "claude-opus-5-5", Effort: "high"}, anthropicEffort, "high"},
		{"anthropic dated snapshot", model.LLMConfig{Provider: "anthropic", Model: "claude-opus-4-5-20251101"}, anthropicEffort, "medium"},
		{"anthropic model without effort", model.LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4-5"}, anthropicEffort, nil},
		{"anthropic haiku without effort", model.LLMConfig{Provider: "anthropic", Model: "claude-haiku-4-5"}, anthropicEffort, nil},
		{"openai default", model.LLMConfig{Provider: "openai", Model: "gpt-5"}, field("reasoning_effort"), "low"},
		{"openai o-series", model.LLMConfig{Provider: "openai", Model: "o4-mini"}, field("reasoning_effort"), "low"},
		{"openai gateway name", model.LLMConfig{Provider: "openai", Model: "openai/gpt-6.2-astra"}, field("reasoning_effort"), "low"},
		{"openai set", model.LLMConfig{Provider: "openai", Model: "gpt-5", Effort: "high"}, field("reasoning_effort"), "high"},
		{"openai param wins", model.LLMConfig{Provider: "openai", Model: "gpt-5", Params: map[string]string{"reasoning_effort": "medium"}}, field("reasoning_effort"), "medium"},
		{"openai non-reasoning model", model.LLMConfig{Provider: "openai", Model: "gpt-4.1"}, field("reasoning_effort"), nil},
		{"openai chat model", model.LLMConfig{Provider: "openai", Model: "gpt-5-chat-latest"}, field("reasoning_effort"), nil},
		{"openai o1-mini", model.LLMConfig{Provider: "openai", Model: "o1-mini"}, field("reasoning_effort"), nil},
		{"ollama default", model.LLMConfig{Provider: "ollama", Model: "gpt-oss:20b"}, field("think"), "low"},
		{"ollama model without levels", model.LLMConfig{Provider: "ollama", Model: "qwen3:8b"}, field("think"), nil},
	}
	for _, tc := range cases {
		for _, schema := range []bool{false, true} {
			name := tc.name
			if schema {
				name += " structured"
			}
			t.Run(name, func(t *testing.T) {
				body := effortSent(t, tc.cfg, schema)
				if got := tc.sent(body); got != tc.want {
					t.Errorf("effort sent = %v, want %v; body %v", got, tc.want, body)
				}
			})
		}
	}
}

func field(name string) func(map[string]any) any {
	return func(body map[string]any) any { return body[name] }
}
