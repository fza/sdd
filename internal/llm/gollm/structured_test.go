package gollm_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	upstreamllm "github.com/teilomillet/gollm/llm"

	gollmrunner "github.com/networkteam/sdd/internal/llm/gollm"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/test"
)

var findingsSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"findings"},
	"properties": map[string]any{
		"findings": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []any{"observation", "severity"},
				"properties": map[string]any{
					"observation": map[string]any{"type": "string"},
					"severity":    map[string]any{"type": "string", "enum": []any{"high", "medium", "low"}},
				},
			},
		},
	},
}

const validFindings = `{"findings":[{"observation":"o","severity":"low"}]}`

func newStructuredRunner(t *testing.T, endpoint, modelName string, mode model.StructuredOutputMode) *gollmrunner.Runner {
	t.Helper()
	runner, err := gollmrunner.NewRunner(model.LLMConfig{
		Provider: "openai", Model: modelName, Endpoint: endpoint, StructuredOutput: mode,
		APIKeys: map[string]string{"openai": "op-notarealkeyaaaaaaaaaa"},
		Retry:   model.RetryConfig{MaxAttempts: 1},
	}, gollmrunner.WithStructuredOutput("findings", findingsSchema))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return runner
}

func TestStructuredOutputModeSendsWhatItNames(t *testing.T) {
	for _, tc := range []struct {
		mode            model.StructuredOutputMode
		wantFormat      string
		wantInstruction bool
	}{
		{model.StructuredOutputJSONSchema, "json_schema", false},
		{model.StructuredOutputJSONObject, "json_object", true},
		{model.StructuredOutputAuto, "json_schema", false},
		{"", "json_schema", false},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			server := test.NewOpenAIServer(t, test.OpenAIBehaviour{Answer: validFindings})
			runner := newStructuredRunner(t, server.URL, "m", tc.mode)

			result, err := runner.Run(context.Background(), llm.Request{SystemPrompt: "SYSTEM", UserPrompt: "USER"})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if result.Text != validFindings {
				t.Errorf("answer should pass through unchanged, got %q", result.Text)
			}
			if got := server.ResponseFormats(); !slices.Equal(got, []string{tc.wantFormat}) {
				t.Errorf("response formats sent should name the mode, got %v", got)
			}
			system := server.SystemMessage(0)
			if !strings.HasPrefix(system, "SYSTEM") {
				t.Errorf("caller's system prompt should lead, got %q", system)
			}
			if hasInstruction := strings.Contains(system, `"findings"`); hasInstruction != tc.wantInstruction {
				t.Errorf("schema in the system prompt should follow json_object mode, got %q", system)
			}
		})
	}
}

func TestStructuredOutputRejectsUnknownMode(t *testing.T) {
	_, err := gollmrunner.NewRunner(model.LLMConfig{
		Provider: "openai", Model: "m", StructuredOutput: "json_whatever",
		APIKeys: map[string]string{"openai": "op-notarealkeyaaaaaaaaaa"},
	})
	if err == nil || !strings.Contains(err.Error(), "json_whatever") {
		t.Errorf("an unknown mode should fail construction naming the value, got %v", err)
	}
}

func TestAutoFallsBackWhenEndpointRefusesJSONSchema(t *testing.T) {
	server := test.NewOpenAIServer(t, test.OpenAIBehaviour{RefuseJSONSchema: true, Answer: validFindings})
	runner := newStructuredRunner(t, server.URL, "glm-5.3-flash", model.StructuredOutputAuto)

	result, err := runner.Run(context.Background(), llm.Request{SystemPrompt: "SYSTEM", UserPrompt: "USER"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Text != validFindings {
		t.Errorf("fallback answer should be served, got %q", result.Text)
	}
	if got := server.ResponseFormats(); !slices.Equal(got, []string{"json_schema", "json_object"}) {
		t.Errorf("a refused json_schema request should be retried once in json_object mode, got %v", got)
	}

	if _, err := runner.Run(context.Background(), llm.Request{UserPrompt: "USER"}); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	fresh := newStructuredRunner(t, server.URL, "glm-5.3-flash", model.StructuredOutputAuto)
	if _, err := fresh.Run(context.Background(), llm.Request{UserPrompt: "USER"}); err != nil {
		t.Fatalf("fresh runner Run: %v", err)
	}
	if got := server.ResponseFormats(); !slices.Equal(got, []string{"json_schema", "json_object", "json_object", "json_object"}) {
		t.Errorf("the refusal should be remembered for the endpoint and model across runners, got %v", got)
	}
}

func TestAutoKeepsJSONSchemaOnUnrelatedBadRequest(t *testing.T) {
	server := test.NewOpenAIServer(t, test.OpenAIBehaviour{
		RejectWith: `{"error":{"message":"max_tokens is too large","type":"invalid_request_error","code":"invalid_value"}}`,
	})
	runner := newStructuredRunner(t, server.URL, "m", model.StructuredOutputAuto)

	_, err := runner.Run(context.Background(), llm.Request{UserPrompt: "USER"})
	if err == nil {
		t.Fatal("an unrelated 400 should fail the call")
	}
	if !strings.Contains(err.Error(), "max_tokens is too large") {
		t.Errorf("the provider's message should reach the caller, got %v", err)
	}
	if got := server.ResponseFormats(); !slices.Equal(got, []string{"json_schema"}) {
		t.Errorf("an unrelated 400 should not trigger the fallback, got %v", got)
	}
}

func TestJSONObjectAnswerOutsideTheSchemaIsAResponseFault(t *testing.T) {
	for _, tc := range []struct {
		name         string
		answer       string
		wantMismatch bool
	}{
		{"invalid JSON", `{"findings":[`, true},
		{"missing required field", `{"findings":[{"observation":"o"}]}`, true},
		{"value outside the enum", `{"findings":[{"observation":"o","severity":"urgent"}]}`, true},
		{"not an object", `[]`, true},
		{"empty answer", ``, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := test.NewOpenAIServer(t, test.OpenAIBehaviour{Answer: tc.answer})
			runner := newStructuredRunner(t, server.URL, "m", model.StructuredOutputJSONObject)

			_, err := runner.Run(context.Background(), llm.Request{UserPrompt: "USER"})
			if err == nil {
				t.Fatal("an answer outside the schema should not pass")
			}
			if errors.Is(err, gollmrunner.ErrSchemaMismatch) != tc.wantMismatch {
				t.Errorf("schema mismatch should be named only for a delivered answer, got %v", err)
			}
			var attributed *llm.Error
			if !errors.As(err, &attributed) || attributed.Identity.Model != "m" {
				t.Errorf("failure should carry the identity, got %v", err)
			}
			var fault *upstreamllm.LLMError
			if !errors.As(err, &fault) || fault.Type != upstreamllm.ErrorTypeResponse {
				t.Errorf("failure should be the response fault an empty answer raises, got %v", err)
			}
		})
	}
}
