package factory_test

import (
	"context"
	"testing"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/test"
)

func TestPreflightParamsTuneOnlyThePreflightCalls(t *testing.T) {
	tests := []struct {
		name      string
		preflight map[string]string
		want      map[llm.Purpose]map[string]any
	}{
		{
			name:      "unset, pre-flight takes llm.params",
			preflight: nil,
			want: map[llm.Purpose]map[string]any{
				llm.PurposePreflight:        {"reasoning_effort": "high", "verbosity": "low"},
				llm.PurposePreflightExtract: {"reasoning_effort": "high", "verbosity": "low"},
				llm.PurposeSummarize:        {"reasoning_effort": "high", "verbosity": "low"},
			},
		},
		{
			name:      "set, laid over llm.params key by key for pre-flight alone",
			preflight: map[string]string{"reasoning_effort": "medium"},
			want: map[llm.Purpose]map[string]any{
				llm.PurposePreflight:        {"reasoning_effort": "medium", "verbosity": "low"},
				llm.PurposePreflightExtract: {"reasoning_effort": "medium", "verbosity": "low"},
				llm.PurposeSummarize:        {"reasoning_effort": "high", "verbosity": "low"},
			},
		},
	}
	purposes := []llm.Purpose{llm.PurposePreflight, llm.PurposePreflightExtract, llm.PurposeSummarize}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := test.NewOpenAIServer(t, test.OpenAIBehaviour{Answer: "ok"})
			cfg := openAIConfig(server.URL)
			cfg.Params = map[string]string{"reasoning_effort": "high", "verbosity": "low"}
			cfg.Preflight.Params = tt.preflight
			runner, err := factory.New(cfg)
			if err != nil {
				t.Fatalf("factory.New: %v", err)
			}
			for _, purpose := range purposes {
				_, err := runner.Run(context.Background(), llm.Request{Purpose: purpose, UserPrompt: "hello"})
				if err != nil {
					t.Fatalf("Run %s: %v", purpose, err)
				}
			}
			requests := server.Requests()
			if len(requests) != len(purposes) {
				t.Fatalf("each call should reach the endpoint once, got %d request(s)", len(requests))
			}
			for i, purpose := range purposes {
				for key, want := range tt.want[purpose] {
					if got := requests[i][key]; got != want {
						t.Errorf("%s call sent %s = %v, want %v", purpose, key, got, want)
					}
				}
			}
		})
	}
}
