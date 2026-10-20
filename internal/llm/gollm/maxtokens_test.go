package gollm_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	gollmrunner "github.com/networkteam/sdd/internal/llm/gollm"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/test"
)

func TestMaxTokensReachesTheRequest(t *testing.T) {
	zero, explicit := 0, 32768
	for _, tc := range []struct {
		name      string
		maxTokens *int
		want      any
	}{
		{"unset sends the default", nil, float64(model.DefaultLLMMaxTokens)},
		{"zero sends no cap", &zero, nil},
		{"explicit cap", &explicit, float64(explicit)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := test.NewOpenAIServer(t, test.OpenAIBehaviour{Answer: "ok"})
			runner, err := gollmrunner.NewRunner(model.LLMConfig{
				Provider: "openai", Model: "glm-5.3-flash", Endpoint: server.URL, MaxTokens: tc.maxTokens,
				APIKeys: map[string]string{"openai": "op-notarealkeyaaaaaaaaaa"},
				Retry:   model.RetryConfig{MaxAttempts: 1},
			})
			if err != nil {
				t.Fatalf("NewRunner: %v", err)
			}

			_, err = runner.Run(context.Background(), llm.Request{UserPrompt: "USER"})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := server.Requests()[0]["max_tokens"]; got != tc.want {
				t.Errorf("max_tokens on the wire should follow the configured key, got %v", got)
			}
		})
	}
}

func TestNegativeMaxTokensIsRefused(t *testing.T) {
	negative := -1
	_, err := gollmrunner.NewRunner(model.LLMConfig{
		Provider: "openai", Model: "m", MaxTokens: &negative,
		APIKeys: map[string]string{"openai": "op-notarealkeyaaaaaaaaaa"},
	})
	if err == nil || !strings.Contains(err.Error(), "llm.max_tokens") {
		t.Fatalf("a negative cap should fail construction naming the key, got %v", err)
	}
}

func TestIncompleteAnswerIsReportedApartFromAProviderFault(t *testing.T) {
	for _, tc := range []struct {
		name      string
		behaviour test.OpenAIBehaviour
		mode      model.StructuredOutputMode
		sentinel  error
		message   string
	}{
		{
			name:      "reasoning spent the cap",
			behaviour: test.OpenAIBehaviour{FinishReason: "length", ReasoningTokens: 7602},
			sentinel:  gollmrunner.ErrAnswerCut,
			message:   "gollm openai: answer cut at the token cap: finish_reason=length, reasoning 7602 tokens, content 0 chars",
		},
		{
			name:      "answer cut mid-JSON in json_object mode",
			behaviour: test.OpenAIBehaviour{Answer: `{"findings":[`, FinishReason: "length", ReasoningTokens: 7602},
			mode:      model.StructuredOutputJSONObject,
			sentinel:  gollmrunner.ErrAnswerCut,
			message:   "gollm openai: answer cut at the token cap: finish_reason=length, reasoning 7602 tokens, content 13 chars",
		},
		{
			name:      "model stopped without answering",
			behaviour: test.OpenAIBehaviour{FinishReason: "stop", ReasoningTokens: 4000},
			sentinel:  gollmrunner.ErrNoAnswer,
			message:   "gollm openai: no answer: finish_reason=stop, reasoning 4000 tokens, content 0 chars",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := test.NewOpenAIServer(t, tc.behaviour)
			runner := newStructuredRunner(t, server.URL, "glm-5.3-flash", tc.mode)

			_, err := runner.Run(context.Background(), llm.Request{UserPrompt: "USER"})
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("the failure should name why the answer is missing, got %v", err)
			}
			if err.Error() != tc.message {
				t.Errorf("the message should carry the finish reason, reasoning and content length, got %q", err.Error())
			}
			var malformed *llm.MalformedAnswerError
			if errors.As(err, &malformed) {
				t.Error("a cut answer should not be offered for a reformat")
			}
			var attributed *llm.Error
			if !errors.As(err, &attributed) || attributed.Identity.Model != "glm-5.3-flash" {
				t.Errorf("failure should carry the identity, got %v", err)
			}
		})
	}
}
