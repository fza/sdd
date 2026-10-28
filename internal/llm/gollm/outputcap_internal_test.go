package gollm

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// Anthropic ignores the base-URL override, so its stand-in for "no cap" is
// unreachable through a wire test without calling the real API.
func TestOutputCap(t *testing.T) {
	zero, explicit := 0, 2048
	for _, tc := range []struct {
		name      string
		provider  string
		maxTokens *int
		want      int
	}{
		{"unset keeps the default", "openai", nil, model.DefaultLLMMaxTokens},
		{"zero sends no cap", "openai", &zero, 0},
		{"zero on anthropic sends the highest accepted cap", "anthropic", &zero, anthropicUncappedMaxTokens},
		{"explicit cap", "anthropic", &explicit, explicit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := outputCap(model.LLMConfig{Provider: tc.provider, MaxTokens: tc.maxTokens})
			if err != nil {
				t.Fatalf("outputCap: %v", err)
			}
			if got != tc.want {
				t.Errorf("the cap should follow the configured key, got %d", got)
			}
		})
	}
}
