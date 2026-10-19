package gollm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/networkteam/slogutils"
	upstream "github.com/teilomillet/gollm"
	upstreamllm "github.com/teilomillet/gollm/llm"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

// ErrSchemaMismatch marks a json_object answer that is not one JSON object
// matching the response schema. It reaches the caller as the same response
// fault an empty answer raises, wrapped in an llm.MalformedAnswerError, so no
// unvalidated object is passed on as a result.
var ErrSchemaMismatch = errors.New("answer does not match the response schema")

// jsonObjectEndpoints remembers, for the rest of the process, every provider,
// endpoint and model that refused a json_schema request, so auto mode pays the
// refused request once rather than on every call. The endpoint is part of the
// key because one model refuses json_schema on one endpoint and accepts it on
// another.
var jsonObjectEndpoints sync.Map

type clientFactory func(formatKey string, formatValue any) (upstream.LLM, error)

// structuredOutput serves a schema-carrying runner in its configured mode.
type structuredOutput struct {
	mode        model.StructuredOutputMode
	endpointKey string
	newClient   clientFactory
	// instruction is appended to the system prompt in json_object mode.
	instruction string
	validator   *jsonschema.Resolved

	mu           sync.Mutex
	schemaClient upstream.LLM
	objectClient upstream.LLM
}

func newStructuredOutput(cfg model.LLMConfig, mode model.StructuredOutputMode, name string, schema map[string]any, newClient clientFactory) (*structuredOutput, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("gollm: encoding response schema %s: %w", name, err)
	}
	var parsed jsonschema.Schema
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("gollm: decoding response schema %s: %w", name, err)
	}
	validator, err := parsed.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("gollm: resolving response schema %s: %w", name, err)
	}

	so := &structuredOutput{
		mode:        mode,
		endpointKey: strings.Join([]string{cfg.Provider, cfg.Endpoint, cfg.OllamaEndpoint, cfg.Model}, "\x00"),
		newClient:   newClient,
		instruction: "Answer with one JSON object matching this JSON Schema, and nothing else:\n" + string(raw),
		validator:   validator,
	}

	// The client a call starts on is built eagerly, so a misconfiguration
	// fails the command rather than its first call.
	if mode == model.StructuredOutputJSONObject {
		so.objectClient, err = newClient(jsonObjectOption(cfg.Provider))
	} else {
		key, value := structuredOutputOption(cfg.Provider, name, schema)
		so.schemaClient, err = newClient(key, value)
	}
	if err != nil {
		return nil, err
	}
	return so, nil
}

func (so *structuredOutput) run(ctx context.Context, r *Runner, req llm.Request) (llm.Result, error) {
	if so.mode == model.StructuredOutputJSONObject {
		return so.runJSONObject(ctx, r, req)
	}
	if so.mode == model.StructuredOutputAuto {
		if _, refused := jsonObjectEndpoints.Load(so.endpointKey); refused {
			return so.runJSONObject(ctx, r, req)
		}
	}

	result, err := r.generate(ctx, so.schemaClient, req)
	if err == nil || so.mode != model.StructuredOutputAuto || !refusesJSONSchema(err) {
		return result, err
	}
	if _, known := jsonObjectEndpoints.LoadOrStore(so.endpointKey, true); !known {
		slogutils.FromContext(ctx).Info("endpoint refuses json_schema for the model, using json_object mode", "provider", r.provider, "model", r.model)
	}
	return so.runJSONObject(ctx, r, req)
}

func (so *structuredOutput) runJSONObject(ctx context.Context, r *Runner, req llm.Request) (llm.Result, error) {
	client, err := so.jsonObjectClient(r.provider)
	if err != nil {
		return llm.Result{}, &llm.Error{Identity: r.identity(), Err: err}
	}

	req.SystemPrompt = strings.TrimSpace(req.SystemPrompt + "\n\n" + so.instruction)
	result, err := r.generate(ctx, client, req)
	if err != nil {
		return llm.Result{}, err
	}
	if err := so.validate(result.Text); err != nil {
		fault := upstreamllm.NewLLMError(upstreamllm.ErrorTypeResponse, "failed to parse response", err)
		return llm.Result{}, r.fault(ctx, &llm.MalformedAnswerError{Text: result.Text, Err: fault})
	}
	return result, nil
}

func (so *structuredOutput) jsonObjectClient(provider string) (upstream.LLM, error) {
	so.mu.Lock()
	defer so.mu.Unlock()
	if so.objectClient != nil {
		return so.objectClient, nil
	}
	client, err := so.newClient(jsonObjectOption(provider))
	if err != nil {
		return nil, err
	}
	so.objectClient = client
	return client, nil
}

func (so *structuredOutput) validate(text string) error {
	var answer any
	err := json.Unmarshal([]byte(strings.TrimSpace(text)), &answer)
	if err != nil {
		return fmt.Errorf("%w: not valid JSON: %w", ErrSchemaMismatch, err)
	}
	if _, isObject := answer.(map[string]any); !isObject {
		return fmt.Errorf("%w: not a JSON object", ErrSchemaMismatch)
	}
	err = so.validator.Validate(answer)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSchemaMismatch, err)
	}
	return nil
}

// refusesJSONSchema reports whether a failed call is the endpoint declining
// json_schema mode for the model, rather than any other bad request.
func refusesJSONSchema(err error) bool {
	var apiErr *upstreamllm.LLMError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		return false
	}
	if apiErr.ProviderCode == "model_not_capable" {
		return true
	}
	message := strings.ToLower(apiErr.ProviderMessage)
	return strings.Contains(message, "json_schema") || strings.Contains(message, "json schema") || strings.Contains(message, "json_object")
}
