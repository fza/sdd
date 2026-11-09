package factory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/networkteam/sdd/internal/model"
)

// Window is a model's context window as the light verifier sizes its calls
// by (20261109-004729-d-cpt-xs5, V-D12): the input tokens it accepts and,
// where the provider reports it, its largest output.
type Window struct {
	Tokens    int
	MaxOutput int
	// Source names where the window came from: the config key or the
	// provider's report.
	Source string
}

const (
	anthropicAPIBase = "https://api.anthropic.com"
	openAIAPIBase    = "https://api.openai.com/v1"
	ollamaAPIBase    = "http://localhost:11434"
)

// WindowDetector resolves a model's window once per process and model: the
// configured llm.context_tokens, else what the provider reports, else a
// refusal naming the key.
type WindowDetector struct {
	Client *http.Client

	mu    sync.Mutex
	known map[string]Window
}

// Detect returns the window of cfg's model.
func (d *WindowDetector) Detect(ctx context.Context, cfg model.LLMConfig) (Window, error) {
	if cfg.ContextTokens > 0 {
		return Window{Tokens: cfg.ContextTokens, Source: "llm.context_tokens"}, nil
	}
	provider := cfg.Provider
	if provider == "" {
		provider = model.DefaultLLMProvider
	}
	modelName := cfg.Model
	if modelName == "" {
		modelName = model.DefaultLLMModel
	}
	key := provider + "\x00" + modelName + "\x00" + cfg.Endpoint + "\x00" + cfg.OllamaEndpoint
	d.mu.Lock()
	window, ok := d.known[key]
	d.mu.Unlock()
	if ok {
		return window, nil
	}
	var err error
	switch provider {
	case "anthropic":
		window, err = d.anthropic(ctx, cfg, modelName)
	case "openai":
		window, err = d.openAICompatible(ctx, cfg, modelName)
	case "ollama":
		window, err = d.ollama(ctx, cfg, modelName)
	}
	if err != nil {
		return Window{}, fmt.Errorf("detecting the context window of %s model %s: %w; set llm.context_tokens to the model's window", provider, modelName, err)
	}
	if window.Tokens <= 0 {
		return Window{}, fmt.Errorf("%s reports no context window for model %s: set llm.context_tokens to the model's window", provider, modelName)
	}
	d.mu.Lock()
	if d.known == nil {
		d.known = map[string]Window{}
	}
	d.known[key] = window
	d.mu.Unlock()
	return window, nil
}

func (d *WindowDetector) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return http.DefaultClient
}

// anthropic reads the Models API's max_input_tokens and max_tokens.
func (d *WindowDetector) anthropic(ctx context.Context, cfg model.LLMConfig, modelName string) (Window, error) {
	base := versionedBase(cfg.Endpoint, anthropicAPIBase, "/messages")
	headers := map[string]string{"anthropic-version": "2023-06-01"}
	if key := cfg.APIKeys["anthropic"]; key != "" {
		headers["x-api-key"] = key
	}
	var answer struct {
		MaxInputTokens int `json:"max_input_tokens"`
		MaxTokens      int `json:"max_tokens"`
	}
	err := d.fetch(ctx, http.MethodGet, base+"/models/"+modelName, headers, nil, &answer)
	if err != nil {
		return Window{}, err
	}
	return Window{Tokens: answer.MaxInputTokens, MaxOutput: answer.MaxTokens, Source: "the Anthropic Models API"}, nil
}

// openAICompatible reads a models listing: OpenRouter's context_length or
// vLLM's max_model_len for the configured model.
func (d *WindowDetector) openAICompatible(ctx context.Context, cfg model.LLMConfig, modelName string) (Window, error) {
	base := versionedBase(cfg.Endpoint, openAIAPIBase, "/chat/completions")
	headers := map[string]string{}
	if key := cfg.APIKeys["openai"]; key != "" {
		headers["Authorization"] = "Bearer " + key
	}
	var answer struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
			MaxModelLen   int    `json:"max_model_len"`
			TopProvider   struct {
				MaxCompletionTokens int `json:"max_completion_tokens"`
			} `json:"top_provider"`
		} `json:"data"`
	}
	err := d.fetch(ctx, http.MethodGet, base+"/models", headers, nil, &answer)
	if err != nil {
		return Window{}, err
	}
	for _, listed := range answer.Data {
		if listed.ID != modelName {
			continue
		}
		switch {
		case listed.ContextLength > 0:
			return Window{Tokens: listed.ContextLength, MaxOutput: listed.TopProvider.MaxCompletionTokens, Source: "the models endpoint's context_length"}, nil
		case listed.MaxModelLen > 0:
			return Window{Tokens: listed.MaxModelLen, Source: "the models endpoint's max_model_len"}, nil
		}
		return Window{}, nil
	}
	return Window{}, fmt.Errorf("the models endpoint does not list %s", modelName)
}

// versionedBase reads endpoint the way gollm does for the chat call (a host,
// a base URL ending in /v1, or the full callPath) and returns its /v1 base;
// an empty endpoint means fallback.
func versionedBase(endpoint, fallback, callPath string) string {
	if endpoint == "" {
		endpoint = fallback
	}
	base := strings.TrimSuffix(strings.TrimRight(endpoint, "/"), callPath)
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base
}

var numCtxParameter = regexp.MustCompile(`(?m)^num_ctx\s+(\d+)`)

// ollama reads /api/show: the num_ctx the model's parameters set, else the
// context length its architecture reports.
func (d *WindowDetector) ollama(ctx context.Context, cfg model.LLMConfig, modelName string) (Window, error) {
	base := strings.TrimRight(cfg.OllamaEndpoint, "/")
	if base == "" {
		base = ollamaAPIBase
	}
	headers := map[string]string{"Content-Type": "application/json"}
	if key := cfg.APIKeys["ollama"]; key != "" {
		headers["Authorization"] = "Bearer " + key
	}
	body, err := json.Marshal(map[string]string{"model": modelName})
	if err != nil {
		return Window{}, err
	}
	var answer struct {
		Parameters string         `json:"parameters"`
		ModelInfo  map[string]any `json:"model_info"`
	}
	err = d.fetch(ctx, http.MethodPost, base+"/api/show", headers, body, &answer)
	if err != nil {
		return Window{}, err
	}
	if match := numCtxParameter.FindStringSubmatch(answer.Parameters); match != nil {
		tokens, err := strconv.Atoi(match[1])
		if err == nil {
			return Window{Tokens: tokens, Source: "the model's num_ctx parameter"}, nil
		}
	}
	for key, value := range answer.ModelInfo {
		if !strings.HasSuffix(key, ".context_length") {
			continue
		}
		if tokens, ok := value.(float64); ok {
			return Window{Tokens: int(tokens), Source: "the model's context_length"}, nil
		}
	}
	return Window{}, nil
}

func (d *WindowDetector) fetch(ctx context.Context, method, url string, headers map[string]string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := d.client().Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s answered %s", method, request.URL.Path, response.Status)
	}
	err = json.Unmarshal(data, out)
	if err != nil {
		return fmt.Errorf("decoding %s: %w", request.URL.Path, err)
	}
	return nil
}
