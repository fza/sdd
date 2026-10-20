package gollm_test

import (
	"context"
	"errors"
	"testing"

	gollmrunner "github.com/networkteam/sdd/internal/llm/gollm"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/test"
)

const meliousBillingCost = `{"energy":"0.103","credits":"0.0000103","paid_with":"energy"}`

func newMeteredRunner(t *testing.T, behaviour test.OpenAIBehaviour) *gollmrunner.Runner {
	t.Helper()
	server := test.NewOpenAIServer(t, behaviour)
	runner, err := gollmrunner.NewRunner(model.LLMConfig{
		Provider: "openai", Model: "glm-5.3-flash", Endpoint: server.URL,
		APIKeys: map[string]string{"openai": "op-notarealkeyaaaaaaaaaa"},
		Retry:   model.RetryConfig{MaxAttempts: 1},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return runner
}

func TestAnswerReportsWhatTheProviderMetered(t *testing.T) {
	runner := newMeteredRunner(t, test.OpenAIBehaviour{
		Answer: "ok", FinishReason: "stop",
		PromptTokens: 19, CachedTokens: 5, ReasoningTokens: 21, BillingCost: meliousBillingCost,
	})

	result, err := runner.Run(context.Background(), llm.Request{UserPrompt: "USER"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := llm.Usage{InputTokens: 19, OutputTokens: 21, CacheReadTokens: 5, ReasoningTokens: 21, BillingCost: meliousBillingCost}
	if result.Usage != want {
		t.Errorf("usage should carry every metered figure the reply reported, got %+v", result.Usage)
	}
	if result.FinishReason != "stop" {
		t.Errorf("the finish reason should be reported, got %q", result.FinishReason)
	}
	if result.Identity.Host != "127.0.0.1" {
		t.Errorf("the identity should name the host the call went to, got %q", result.Identity.Host)
	}
}

func TestCutAnswerCarriesWhatTheProviderMetered(t *testing.T) {
	runner := newMeteredRunner(t, test.OpenAIBehaviour{
		FinishReason: "length", PromptTokens: 900, ReasoningTokens: 4096, BillingCost: meliousBillingCost,
	})

	_, err := runner.Run(context.Background(), llm.Request{UserPrompt: "USER"})

	attributed, ok := errors.AsType[*llm.Error](err)
	if !ok {
		t.Fatalf("a cut answer should fail attributed, got %v", err)
	}
	if attributed.Usage.ReasoningTokens != 4096 || attributed.Usage.InputTokens != 900 || attributed.Usage.BillingCost != meliousBillingCost {
		t.Errorf("a billed reply that failed should carry its usage, got %+v", attributed.Usage)
	}
	if attributed.FinishReason != "length" {
		t.Errorf("a cut answer should report why it stopped, got %q", attributed.FinishReason)
	}
}

func TestMalformedAnswerCarriesWhatTheProviderMetered(t *testing.T) {
	server := test.NewOpenAIServer(t, test.OpenAIBehaviour{
		Answer: "not json", FinishReason: "stop", PromptTokens: 40, ReasoningTokens: 12, BillingCost: meliousBillingCost,
	})
	runner, err := gollmrunner.NewRunner(model.LLMConfig{
		Provider: "openai", Model: "glm-5.3-flash", Endpoint: server.URL,
		APIKeys:          map[string]string{"openai": "op-notarealkeyaaaaaaaaaa"},
		Retry:            model.RetryConfig{MaxAttempts: 1},
		StructuredOutput: model.StructuredOutputJSONObject,
	}, gollmrunner.WithStructuredOutput("answer", map[string]any{"type": "object"}))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	_, err = runner.Run(context.Background(), llm.Request{UserPrompt: "USER"})

	attributed, ok := errors.AsType[*llm.Error](err)
	if !ok {
		t.Fatalf("a malformed answer should fail attributed, got %v", err)
	}
	if attributed.Usage.InputTokens != 40 || attributed.Usage.BillingCost != meliousBillingCost || attributed.FinishReason != "stop" {
		t.Errorf("a billed reply rejected as malformed should carry its usage, got %+v %q", attributed.Usage, attributed.FinishReason)
	}
}

func TestIdentityNamesTheEndpointHost(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  model.LLMConfig
		want string
	}{
		{"openai default", model.LLMConfig{Provider: "openai", APIKeys: map[string]string{"openai": "sk-notarealkeyaaaaaaaaaaaaaaaaaaaa"}}, "api.openai.com"},
		{"openai-compatible endpoint", model.LLMConfig{Provider: "openai", Endpoint: "https://api.melious.ai/v1/chat/completions", APIKeys: map[string]string{"openai": "op-notarealkeyaaaaaaaaaa"}}, "api.melious.ai"},
		{"anthropic default", model.LLMConfig{Provider: "anthropic", APIKeys: map[string]string{"anthropic": "sk-ant-notarealkeyaaaaaaaaaa"}}, "api.anthropic.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.Model = "m"
			tc.cfg.Retry = model.RetryConfig{MaxAttempts: 1}
			runner, err := gollmrunner.NewRunner(tc.cfg)
			if err != nil {
				t.Fatalf("NewRunner: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			_, err = runner.Run(ctx, llm.Request{UserPrompt: "USER"})

			attributed, ok := errors.AsType[*llm.Error](err)
			if !ok {
				t.Fatalf("a call that never left should fail attributed, got %v", err)
			}
			if attributed.Identity.Host != tc.want {
				t.Errorf("the identity should name the provider's host, got %q", attributed.Identity.Host)
			}
		})
	}
}
