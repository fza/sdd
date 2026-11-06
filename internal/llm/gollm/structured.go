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

// structuredOutput serves a request carrying a ResponseSchema in the
// configured mode. json_schema mode is gollm's own schema path; json_object
// mode asks for any JSON object, states the schema in the system prompt and
// validates the answer here.
type structuredOutput struct {
	mode        model.StructuredOutputMode
	endpointKey string
	provider    string
	newClient   clientFactory

	mu           sync.Mutex
	objectClient upstream.LLM
	validators   map[string]*jsonschema.Resolved
}

func newStructuredOutput(cfg model.LLMConfig, mode model.StructuredOutputMode, newClient clientFactory) (*structuredOutput, error) {
	so := &structuredOutput{
		mode:        mode,
		endpointKey: strings.Join([]string{cfg.Provider, cfg.Endpoint, cfg.OllamaEndpoint, cfg.Model}, "\x00"),
		provider:    cfg.Provider,
		newClient:   newClient,
		validators:  map[string]*jsonschema.Resolved{},
	}
	// A json_object runner builds its client eagerly, so a misconfiguration
	// fails the command rather than its first call.
	if mode == model.StructuredOutputJSONObject {
		if _, err := so.jsonObjectClient(); err != nil {
			return nil, err
		}
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

	result, err := r.generate(ctx, r.client, req, upstream.WithResponseSchema(req.ResponseSchema))
	if err == nil || so.mode != model.StructuredOutputAuto || !refusesJSONSchema(err) {
		return result, err
	}
	if _, known := jsonObjectEndpoints.LoadOrStore(so.endpointKey, true); !known {
		slogutils.FromContext(ctx).Info("endpoint refuses json_schema for the model, using json_object mode", "provider", r.provider, "model", r.model)
	}
	return so.runJSONObject(ctx, r, req)
}

func (so *structuredOutput) runJSONObject(ctx context.Context, r *Runner, req llm.Request) (llm.Result, error) {
	client, err := so.jsonObjectClient()
	if err != nil {
		return llm.Result{}, &llm.Error{Identity: r.identity(), Err: err}
	}
	validator, err := so.validator(req.ResponseSchema)
	if err != nil {
		return llm.Result{}, &llm.Error{Identity: r.identity(), Err: err}
	}

	instruction := "Answer with one JSON object matching this JSON Schema, and nothing else:\n" + string(req.ResponseSchema)
	req.SystemPrompt = strings.TrimSpace(req.SystemPrompt + "\n\n" + instruction)
	result, err := r.generate(ctx, client, req)
	if err != nil {
		return llm.Result{}, err
	}
	if err := validate(validator, result.Text); err != nil {
		fault := upstreamllm.NewLLMError(upstreamllm.ErrorTypeResponse, "failed to parse response", err)
		return llm.Result{}, r.rejectedReply(ctx, &llm.MalformedAnswerError{Text: result.Text, Err: fault}, result.Usage, result.FinishReason)
	}
	return result, nil
}

func (so *structuredOutput) jsonObjectClient() (upstream.LLM, error) {
	so.mu.Lock()
	defer so.mu.Unlock()
	if so.objectClient != nil {
		return so.objectClient, nil
	}
	client, err := so.newClient(jsonObjectOption(so.provider))
	if err != nil {
		return nil, err
	}
	so.objectClient = client
	return client, nil
}

// validator resolves a response schema once per distinct schema.
func (so *structuredOutput) validator(schema json.RawMessage) (*jsonschema.Resolved, error) {
	so.mu.Lock()
	defer so.mu.Unlock()
	if resolved, ok := so.validators[string(schema)]; ok {
		return resolved, nil
	}
	var parsed jsonschema.Schema
	if err := json.Unmarshal(schema, &parsed); err != nil {
		return nil, fmt.Errorf("gollm: decoding response schema: %w", err)
	}
	resolved, err := parsed.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("gollm: resolving response schema: %w", err)
	}
	so.validators[string(schema)] = resolved
	return resolved, nil
}

func validate(validator *jsonschema.Resolved, text string) error {
	var answer any
	err := json.Unmarshal([]byte(strings.TrimSpace(text)), &answer)
	if err != nil {
		return fmt.Errorf("%w: not valid JSON: %w", ErrSchemaMismatch, err)
	}
	if _, isObject := answer.(map[string]any); !isObject {
		return fmt.Errorf("%w: not a JSON object", ErrSchemaMismatch)
	}
	err = validator.Validate(answer)
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
