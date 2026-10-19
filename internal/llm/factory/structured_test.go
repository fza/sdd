package factory_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/basefacts"
	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/test"
)

func openAIConfig(endpoint string) model.LLMConfig {
	return model.LLMConfig{
		Provider: "openai", Model: "glm-5.3-flash", Endpoint: endpoint,
		APIKeys:      map[string]string{"openai": "op-notarealkeyaaaaaaaaaa"},
		RateLimitRPS: 1000,
		Retry:        model.RetryConfig{MaxAttempts: 1},
	}
}

func TestPreflightPassesOnEndpointServingJSONObjectOnly(t *testing.T) {
	server := test.NewOpenAIServer(t, test.OpenAIBehaviour{
		RefuseJSONSchema: true,
		Answer:           `{"findings":[{"observation":"The signal states one observation.","category":"scope","severity":"low"}]}`,
	})
	runner, err := factory.New(openAIConfig(server.URL))
	if err != nil {
		t.Fatalf("factory.New: %v", err)
	}

	fact := &model.Entry{ID: basefacts.RefKindsFactID, Type: model.TypeSignal, Layer: model.LayerProcess, Kind: model.KindFact, Content: "ref-kind vocabulary stub"}
	proposed := &model.Entry{Type: model.TypeSignal, Layer: model.LayerTactical, Content: "The pre-flight runs against an endpoint serving json_object only."}

	result, err := llmops.Preflight(context.Background(), runner, proposed, model.NewGraph([]*model.Entry{fact}), "")
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Category != "scope" {
		t.Errorf("findings from the json_object answer should parse, got %+v", result.Findings)
	}
	if got := server.ResponseFormats(); !slices.Equal(got, []string{"json_schema", "json_object"}) {
		t.Errorf("pre-flight should fall back from json_schema to json_object, got %v", got)
	}
}

func TestPreflightReformatsAJSONObjectAnswerOutsideTheSchema(t *testing.T) {
	server := test.NewOpenAIServer(t, test.OpenAIBehaviour{Answers: []string{
		`{"findings":[{"observation":"The signal states one observation.","category":"scope","severity":"none"}]}`,
		`{"findings":[{"observation":"The signal states one observation.","category":"scope","severity":"low"}]}`,
	}})
	cfg := openAIConfig(server.URL)
	cfg.StructuredOutput = model.StructuredOutputJSONObject
	runner, err := factory.New(cfg)
	if err != nil {
		t.Fatalf("factory.New: %v", err)
	}

	fact := &model.Entry{ID: basefacts.RefKindsFactID, Type: model.TypeSignal, Layer: model.LayerProcess, Kind: model.KindFact, Content: "ref-kind vocabulary stub"}
	proposed := &model.Entry{Type: model.TypeSignal, Layer: model.LayerTactical, Content: "The pre-flight runs against an endpoint serving json_object only."}

	result, err := llmops.Preflight(context.Background(), runner, proposed, model.NewGraph([]*model.Entry{fact}), "")
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Severity != llmops.SeverityLow {
		t.Errorf("findings should come from the validated reformat, got %+v", result.Findings)
	}
	if got := len(server.Requests()); got != 2 {
		t.Errorf("an answer outside the schema should cost one reformat call, got %d request(s)", got)
	}
	if !strings.Contains(server.UserMessage(1), `"severity":"none"`) {
		t.Errorf("the reformat should carry the rejected answer, got %q", server.UserMessage(1))
	}
}

func TestFallbackStaysWithinTheCallTimeout(t *testing.T) {
	const bound = 300 * time.Millisecond
	for _, purpose := range []llm.Purpose{llm.PurposePreflight, llm.PurposePreflightExtract} {
		t.Run(string(purpose), func(t *testing.T) {
			server := test.NewOpenAIServer(t, test.OpenAIBehaviour{RefuseJSONSchema: true, Answer: `{"findings":[]}`, Delay: 200 * time.Millisecond})
			cfg := openAIConfig(server.URL)
			cfg.Model = "glm-timeout-" + string(purpose)
			cfg.Timeout = bound.String()
			cfg.ExtractTimeout = bound.String()
			runner, err := factory.New(cfg)
			if err != nil {
				t.Fatalf("factory.New: %v", err)
			}

			started := time.Now()
			_, err = runner.Run(context.Background(), llm.Request{Purpose: purpose, UserPrompt: "x"})
			elapsed := time.Since(started)
			if err == nil {
				t.Fatal("refused request and fallback should share one timeout")
			}
			if elapsed >= 2*bound {
				t.Errorf("refused request and fallback should not outlast twice the timeout, took %v", elapsed)
			}
			if got := server.ResponseFormats(); !slices.Equal(got, []string{"json_schema", "json_object"}) {
				t.Errorf("the fallback should have been attempted, got %v", got)
			}
		})
	}
}
