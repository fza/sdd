package factory_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/pkg/llm"
)

// recordingEndpoint answers every OpenAI chat completion with "ok" and keeps
// each request body in arrival order.
func recordingEndpoint(t *testing.T) (url string, requests func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	t.Cleanup(server.Close)
	return server.URL, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), bodies...)
	}
}

func TestPreflightParamsTuneOnlyThePreflightCalls(t *testing.T) {
	tests := []struct {
		name      string
		preflight map[string]string
		want      map[llm.Purpose]map[string]any
	}{
		{
			name: "unset, pre-flight takes llm.params",
			want: map[llm.Purpose]map[string]any{
				llm.PurposePreflight: {"reasoning_effort": "high", "verbosity": "low"},
				llm.PurposeSummarize: {"reasoning_effort": "high", "verbosity": "low"},
			},
		},
		{
			name:      "set, laid over llm.params key by key for pre-flight alone",
			preflight: map[string]string{"reasoning_effort": "medium"},
			want: map[llm.Purpose]map[string]any{
				llm.PurposePreflight: {"reasoning_effort": "medium", "verbosity": "low"},
				llm.PurposeSummarize: {"reasoning_effort": "high", "verbosity": "low"},
			},
		},
	}
	purposes := []llm.Purpose{llm.PurposePreflight, llm.PurposeSummarize}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, requests := recordingEndpoint(t)
			cfg := model.LLMConfig{
				Provider: "openai",
				Model:    "m",
				Endpoint: url,
				APIKeys:  map[string]string{"openai": "op-notarealkeyaaaaaaaaaa"},
				Params:   map[string]string{"reasoning_effort": "high", "verbosity": "low"},
				Preflight: model.LLMPreflightConfig{
					Params: tt.preflight,
				},
			}
			runner, err := factory.New(cfg)
			if err != nil {
				t.Fatalf("factory.New: %v", err)
			}
			for _, purpose := range purposes {
				if _, err := runner.Run(secrets.WithNoNewParts(context.Background()), llm.Request{Purpose: purpose, UserPrompt: "hello"}); err != nil {
					t.Fatalf("Run %s: %v", purpose, err)
				}
			}
			sent := requests()
			if len(sent) != len(purposes) {
				t.Fatalf("each call should reach the endpoint once, got %d request(s)", len(sent))
			}
			for i, purpose := range purposes {
				for key, want := range tt.want[purpose] {
					if got := sent[i][key]; got != want {
						t.Errorf("the %s call should send the params meant for it, got %s = %v", purpose, key, got)
					}
				}
			}
		})
	}
}
