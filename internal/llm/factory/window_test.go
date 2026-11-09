package factory_test

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/test"
)

// windowServer answers path with the named fixture and counts the calls.
func windowServer(t *testing.T, path, fixture string, check func(*http.Request)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	body, err := fs.ReadFile(test.LLMWindowFixtures(), fixture)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		if check != nil {
			check(r)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func TestWindowDetect_PerProvider(t *testing.T) {
	for _, tc := range []struct {
		name      string
		path      string
		fixture   string
		cfg       func(base string) model.LLMConfig
		tokens    int
		maxOutput int
	}{
		{"anthropic models API", "/v1/models/claude-sonnet-4-6", "anthropic-model.json",
			func(base string) model.LLMConfig {
				return model.LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4-6", Endpoint: base, APIKeys: map[string]string{"anthropic": "k"}}
			}, 200000, 64000},
		{"openrouter context_length", "/api/v1/models", "openrouter-models.json",
			func(base string) model.LLMConfig {
				return model.LLMConfig{Provider: "openai", Model: "z-ai/glm-4.6", Endpoint: base + "/api/v1"}
			}, 202752, 131072},
		{"vllm max_model_len", "/v1/models", "vllm-models.json",
			func(base string) model.LLMConfig {
				return model.LLMConfig{Provider: "openai", Model: "Qwen/Qwen3-32B", Endpoint: base + "/v1"}
			}, 32768, 0},
		{"ollama context_length", "/api/show", "ollama-show.json",
			func(base string) model.LLMConfig {
				return model.LLMConfig{Provider: "ollama", Model: "qwen3:8b", OllamaEndpoint: base}
			}, 40960, 0},
		{"ollama num_ctx parameter", "/api/show", "ollama-show-num-ctx.json",
			func(base string) model.LLMConfig {
				return model.LLMConfig{Provider: "ollama", Model: "qwen3:8b", OllamaEndpoint: base}
			}, 16384, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := windowServer(t, tc.path, tc.fixture, nil)
			detector := &factory.WindowDetector{Client: server.Client()}
			window, err := detector.Detect(t.Context(), tc.cfg(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			if window.Tokens != tc.tokens || window.MaxOutput != tc.maxOutput {
				t.Fatalf("window should be read from the provider's answer, got %+v", window)
			}
		})
	}
}

func TestWindowDetect_ConfiguredKeyWinsWithoutACall(t *testing.T) {
	server, calls := windowServer(t, "/v1/models/claude-sonnet-4-6", "anthropic-model.json", nil)
	detector := &factory.WindowDetector{Client: server.Client()}
	window, err := detector.Detect(t.Context(), model.LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4-6", Endpoint: server.URL, ContextTokens: 50000})
	if err != nil {
		t.Fatal(err)
	}
	if window.Tokens != 50000 || window.Source != "llm.context_tokens" || calls.Load() != 0 {
		t.Fatalf("the configured window should stand without asking the provider, got %+v after %d calls", window, calls.Load())
	}
}

func TestWindowDetect_AsksOncePerModel(t *testing.T) {
	server, calls := windowServer(t, "/v1/models/claude-sonnet-4-6", "anthropic-model.json", func(r *http.Request) {
		if r.Header.Get("x-api-key") != "k" || r.Header.Get("anthropic-version") == "" {
			t.Errorf("the models request should carry the key and the API version")
		}
	})
	detector := &factory.WindowDetector{Client: server.Client()}
	cfg := model.LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4-6", Endpoint: server.URL, APIKeys: map[string]string{"anthropic": "k"}}
	for range 3 {
		_, err := detector.Detect(t.Context(), cfg)
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("the window should be asked for once per process and model, asked %d times", calls.Load())
	}
}

func TestWindowDetect_RefusesNamingTheKey(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(failing.Close)
	unlisted, _ := windowServer(t, "/v1/models", "vllm-models.json", nil)
	for _, tc := range []struct {
		name string
		cfg  model.LLMConfig
	}{
		{"claude-cli reports nothing", model.LLMConfig{Provider: "claude-cli", Model: "claude-sonnet-4-6"}},
		{"plain endpoint fails", model.LLMConfig{Provider: "openai", Model: "gpt-x", Endpoint: failing.URL}},
		{"model not listed", model.LLMConfig{Provider: "openai", Model: "gpt-x", Endpoint: unlisted.URL + "/v1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detector := &factory.WindowDetector{Client: failing.Client()}
			_, err := detector.Detect(t.Context(), tc.cfg)
			if err == nil || !strings.Contains(err.Error(), "set llm.context_tokens") {
				t.Fatalf("an unknown window should be refused naming the key, got %v", err)
			}
		})
	}
}

// The window request derives the models URL from every endpoint form the
// chat call accepts: a host, a versioned base URL or the full call path.
func TestWindowDetect_AcceptsEveryEndpointForm(t *testing.T) {
	for _, tc := range []struct {
		name     string
		path     string
		fixture  string
		provider string
		model    string
		suffix   string
	}{
		{"openai host", "/v1/models", "vllm-models.json", "openai", "Qwen/Qwen3-32B", ""},
		{"openai versioned base", "/v1/models", "vllm-models.json", "openai", "Qwen/Qwen3-32B", "/v1/"},
		{"openai chat completions path", "/v1/models", "vllm-models.json", "openai", "Qwen/Qwen3-32B", "/v1/chat/completions"},
		{"anthropic host", "/v1/models/claude-sonnet-4-6", "anthropic-model.json", "anthropic", "claude-sonnet-4-6", ""},
		{"anthropic versioned base", "/v1/models/claude-sonnet-4-6", "anthropic-model.json", "anthropic", "claude-sonnet-4-6", "/v1"},
		{"anthropic messages path", "/v1/models/claude-sonnet-4-6", "anthropic-model.json", "anthropic", "claude-sonnet-4-6", "/v1/messages/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, calls := windowServer(t, tc.path, tc.fixture, nil)
			detector := &factory.WindowDetector{Client: server.Client()}
			window, err := detector.Detect(t.Context(), model.LLMConfig{Provider: tc.provider, Model: tc.model, Endpoint: server.URL + tc.suffix})
			if err != nil {
				t.Fatal(err)
			}
			if window.Tokens <= 0 || calls.Load() != 1 {
				t.Fatalf("the window should be read from the models URL the endpoint leads to, got %+v after %d calls", window, calls.Load())
			}
		})
	}
}
