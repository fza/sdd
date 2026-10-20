// Package gollm implements llm.Runner on top of github.com/teilomillet/gollm,
// providing a unified adapter for Anthropic API, OpenAI, Ollama, and other
// providers supported by gollm. This is the opt-in alternative to the
// claude-cli bridge; selected via the llm.provider config setting.
package gollm

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	upstream "github.com/teilomillet/gollm"
	upstreamllm "github.com/teilomillet/gollm/llm"
	"github.com/teilomillet/gollm/providers"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

// ErrAnswerCut marks a call the model ended at the output token cap. A
// reasoning model spends its thinking against that cap, so the answer comes
// back cut short or missing; llm.max_tokens raises or lifts the cap.
var ErrAnswerCut = errors.New("answer cut at the token cap")

// ErrNoAnswer marks a response the provider delivered whole, ended for a
// reason other than the cap, that carries no answer.
var ErrNoAnswer = errors.New("no answer")

// anthropicUncappedMaxTokens stands in for "no cap" on Anthropic, whose API
// requires max_tokens. It is the highest output limit every current Claude
// model accepts.
const anthropicUncappedMaxTokens = 32000

// Runner wraps a gollm client and adapts it to llm.Runner. When the provider
// is Anthropic, the SystemPrompt is passed with ephemeral cache control so
// the stable prefix is server-cached across calls in the same 5-minute
// window — a significant speedup for batch operations like
// sdd summarize --all.
type Runner struct {
	client   upstream.LLM
	provider string
	model    string
	variant  string
	host     string
	useCache bool

	// structured is nil for a runner carrying no schema.
	structured *structuredOutput
}

// Option configures a Runner at construction.
type Option func(*options)

type options struct {
	schemaName string
	schema     map[string]any
}

// WithStructuredOutput constrains responses to schema where the provider has
// a known wire key for it. Providers without one (anthropic, and any not
// listed in structuredOutputOption) are left unconstrained rather than sent a
// key they would echo into the prompt or reject.
func WithStructuredOutput(name string, schema map[string]any) Option {
	return func(o *options) {
		o.schemaName = name
		o.schema = schema
	}
}

// NewRunner constructs a gollm-backed Runner from an LLMConfig. Provider
// must be one of the gollm-supported providers (e.g. "anthropic", "openai",
// "ollama"). Returns a typed error when required config is missing (API
// key for remote providers).
func NewRunner(cfg model.LLMConfig, opts ...Option) (*Runner, error) {
	var o options
	for _, apply := range opts {
		apply(&o)
	}

	if cfg.Provider == "" {
		return nil, fmt.Errorf("gollm: provider not configured")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("gollm: model not configured for provider %q", cfg.Provider)
	}

	// Resolve the retry policy (provider-aware backoff lives in the gollm
	// fork). We previously forced SetMaxRetries(0) because gollm's stock
	// retry logged every attempt at WARN and flooded batch summarize; the
	// fork classifies transient vs permanent errors, honors server-provided
	// Retry-After, and logs attempts at Debug — so retries are now safe to
	// enable. gollm counts retries *after* the first attempt, hence -1.
	maxAttempts, baseDelay, maxDelay, err := cfg.Retry.Resolved()
	if err != nil {
		return nil, fmt.Errorf("gollm: %w", err)
	}

	maxTokens, err := outputCap(cfg)
	if err != nil {
		return nil, fmt.Errorf("gollm: %w", err)
	}

	cfgOpts := []upstream.ConfigOption{
		upstream.SetProvider(cfg.Provider),
		upstream.SetModel(cfg.Model),
		upstream.SetMaxRetries(maxAttempts - 1),
		upstream.SetRetryDelay(baseDelay),
		upstream.SetMaxRetryDelay(maxDelay),
	}
	if maxTokens == 0 {
		cfgOpts = append(cfgOpts, upstream.SetNoMaxTokens())
	} else {
		cfgOpts = append(cfgOpts, upstream.SetMaxTokens(maxTokens))
	}

	// API key is required for remote providers.
	if needsAPIKey(cfg.Provider) {
		key := cfg.APIKeys[cfg.Provider]
		if key == "" {
			return nil, fmt.Errorf("gollm: api key missing for provider %q — run `sdd config set llm.api_keys.%s <key>`", cfg.Provider, cfg.Provider)
		}
		cfgOpts = append(cfgOpts, upstream.SetAPIKey(key))
	}

	// Ollama endpoint override. A key is optional: local deployments run
	// without one, Ollama Cloud (ollama.com) authenticates with it as a
	// Bearer token.
	if cfg.Provider == "ollama" {
		if cfg.OllamaEndpoint != "" {
			cfgOpts = append(cfgOpts, upstream.SetOllamaEndpoint(cfg.OllamaEndpoint))
		}
		if key := cfg.APIKeys["ollama"]; key != "" {
			cfgOpts = append(cfgOpts, upstream.SetAPIKey(key))
		}
	}

	if cfg.Provider == "openai" && cfg.Endpoint != "" {
		cfgOpts = append(cfgOpts, upstream.SetOpenAIEndpoint(cfg.Endpoint))
	}

	// Enable Anthropic prompt caching — sends the anthropic-beta header so
	// cache_control blocks on system prompts are honored server-side.
	useCache := cfg.Provider == "anthropic"
	if useCache {
		cfgOpts = append(cfgOpts, upstream.SetEnableCaching(true))
	}

	// Timeout flows into gollm's HTTP client.
	if cfg.Timeout != "" {
		d, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return nil, fmt.Errorf("gollm: parsing timeout %q: %w", cfg.Timeout, err)
		}
		cfgOpts = append(cfgOpts, upstream.SetTimeout(d))
	}

	newClient := func(formatKey string, formatValue any) (upstream.LLM, error) {
		client, err := upstream.NewLLM(cfgOpts...)
		if err != nil {
			// gollm validates ollama by probing the endpoint; on failure it
			// reports a generic apikey validation error. Rewrap as a targeted
			// "unreachable" message so users see the actionable hint.
			if cfg.Provider == "ollama" && strings.Contains(err.Error(), "apikey") {
				endpoint := cfg.OllamaEndpoint
				if endpoint == "" {
					endpoint = "http://localhost:11434"
				}
				return nil, fmt.Errorf("gollm ollama: provider unreachable at %s (is Ollama running?)", endpoint)
			}
			return nil, fmt.Errorf("gollm: creating client: %w", err)
		}

		// Behaviour-affecting params go to the provider verbatim; gollm forwards
		// unknown option keys straight into the request body.
		for _, k := range sortedKeys(cfg.Params) {
			client.SetOption(k, cfg.Params[k])
		}
		if formatKey != "" {
			client.SetOption(formatKey, formatValue)
		}
		return client, nil
	}

	mode, err := model.ParseStructuredOutputMode(string(cfg.StructuredOutput))
	if err != nil {
		return nil, fmt.Errorf("gollm: %w", err)
	}

	runner := &Runner{
		provider: cfg.Provider,
		model:    cfg.Model,
		variant:  canonicalVariant(cfg.Params),
		host:     endpointHost(cfg.Provider, cfg.Model, cfgOpts),
		useCache: useCache,
	}

	if o.schema == nil {
		runner.client, err = newClient("", nil)
		if err != nil {
			return nil, err
		}
		return runner, nil
	}

	runner.structured, err = newStructuredOutput(cfg, mode, o.schemaName, o.schema, newClient)
	if err != nil {
		return nil, err
	}
	return runner, nil
}

// endpointHost names the host gollm sends this configuration's calls to. It
// asks a provider built the way gollm builds its own, so a provider's default
// endpoint and gollm's environment overrides resolve exactly as on the wire.
// Empty when the provider or its endpoint cannot be resolved.
func endpointHost(providerName, modelName string, cfgOpts []upstream.ConfigOption) string {
	gollmConfig, err := upstream.LoadConfig()
	if err != nil {
		return ""
	}
	for _, apply := range cfgOpts {
		apply(gollmConfig)
	}
	provider, err := providers.GetDefaultRegistry().Get(providerName, "", modelName, nil)
	if err != nil {
		return ""
	}
	provider.SetDefaultOptions(gollmConfig)
	endpoint, err := url.Parse(provider.Endpoint())
	if err != nil {
		return ""
	}
	return endpoint.Hostname()
}

// outputCap is the max_tokens a call carries, 0 sending none.
func outputCap(cfg model.LLMConfig) (int, error) {
	maxTokens, err := cfg.ResolvedMaxTokens()
	if err != nil {
		return 0, err
	}
	if maxTokens == 0 && cfg.Provider == "anthropic" {
		return anthropicUncappedMaxTokens, nil
	}
	return maxTokens, nil
}

// canonicalVariant renders the behaviour-affecting params as a stable label —
// sorted key=value pairs — so the same configuration always groups to the same
// row across runs and machines.
func canonicalVariant(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	parts := make([]string, 0, len(params))
	for _, k := range sortedKeys(params) {
		parts = append(parts, k+"="+params[k])
	}
	return strings.Join(parts, ",")
}

func sortedKeys(params map[string]string) []string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// identity names the configured provider and model. Both are known at
// construction, so they attribute a call whatever the response turns out to be.
func (r *Runner) identity() llm.Identity {
	return llm.Identity{Provider: r.provider, Model: r.model, Variant: r.variant, Host: r.host}
}

// Run sends the request to the underlying gollm client. When caching is
// enabled (Anthropic), the SystemPrompt is tagged ephemeral so the server
// caches the prefix. Other providers ignore the cache hint but still see
// the split system/user message pair. Failures carry the identity through the
// typed llm.Error so they measure like successes.
func (r *Runner) Run(ctx context.Context, req llm.Request) (llm.Result, error) {
	if r.structured != nil {
		return r.structured.run(ctx, r, req)
	}
	return r.generate(ctx, r.client, req)
}

func (r *Runner) generate(ctx context.Context, client upstream.LLM, req llm.Request) (llm.Result, error) {
	var opts []upstream.PromptOption
	if req.SystemPrompt != "" {
		cacheType := upstream.CacheType("")
		if r.useCache {
			cacheType = upstream.CacheTypeEphemeral
		}
		opts = append(opts, upstream.WithSystemPrompt(req.SystemPrompt, cacheType))
	}

	prompt := upstream.NewPrompt(req.UserPrompt, opts...)

	resp, err := client.GenerateWithUsage(ctx, prompt)
	if err != nil {
		var noAnswer *upstreamllm.NoAnswerError
		if errors.As(err, &noAnswer) {
			err = incompleteAnswer(noAnswer.Truncated(), noAnswer.FinishReason, noAnswer.Usage, "")
			return llm.Result{}, r.rejectedReply(ctx, err, usageFromResponse(noAnswer.Usage), noAnswer.FinishReason)
		}
		return llm.Result{}, r.fault(ctx, err)
	}
	if resp.Truncated() {
		err := incompleteAnswer(true, resp.FinishReason, resp.Usage, resp.Content)
		return llm.Result{}, r.rejectedReply(ctx, err, usageFromResponse(resp.Usage), resp.FinishReason)
	}

	return llm.Result{
		Text:         resp.Content,
		Identity:     r.identity(),
		Usage:        usageFromResponse(resp.Usage),
		FinishReason: resp.FinishReason,
	}, nil
}

// incompleteAnswer reports an answer the model cut or never gave, with what it
// spent reasoning, so a cap reads apart from a provider fault.
func incompleteAnswer(cut bool, finishReason string, usage *upstream.Usage, content string) error {
	reasoning := 0
	if usage != nil {
		reasoning = usage.ReasoningTokens
	}
	sentinel := ErrNoAnswer
	if cut {
		sentinel = ErrAnswerCut
	}
	return fmt.Errorf("%w: finish_reason=%s, reasoning %d tokens, content %d chars", sentinel, finishReason, reasoning, utf8.RuneCountInString(content))
}

// fault wraps a failed call in the typed llm.Error carrying the identity.
func (r *Runner) fault(ctx context.Context, err error) *llm.Error {
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("gollm %s: timed out", r.provider)
	} else {
		err = fmt.Errorf("gollm %s: %w", r.provider, err)
	}
	return &llm.Error{Identity: r.identity(), Err: err}
}

// rejectedReply is fault for a reply the provider delivered and billed, so the
// failure carries what the reply reported.
func (r *Runner) rejectedReply(ctx context.Context, err error, usage llm.Usage, finishReason string) error {
	fault := r.fault(ctx, err)
	fault.Usage = usage
	fault.FinishReason = finishReason
	return fault
}

// usageFromResponse maps a gollm Response.Usage into the common llm.Usage.
// It prefers the Anthropic-style input/output counts and falls back to the
// OpenAI-style prompt/completion counts. Zero when the provider reported no
// usage — Ollama reports its counts as prompt_eval_count and eval_count,
// outside the usage object the fork's parser reads — which costs only the
// token figures now that identity travels on the Result.
func usageFromResponse(u *upstream.Usage) llm.Usage {
	if u == nil {
		return llm.Usage{}
	}
	in := u.InputTokens
	if in == 0 {
		in = u.PromptTokens
	}
	out := u.OutputTokens
	if out == 0 {
		out = u.CompletionTokens
	}
	return llm.Usage{
		InputTokens:       in,
		OutputTokens:      out,
		CacheReadTokens:   u.CacheReadInputTokens,
		CacheCreateTokens: u.CacheCreationInputTokens,
		ReasoningTokens:   u.ReasoningTokens,
		BillingCost:       string(u.BillingCost),
	}
}

// structuredOutputOption maps a schema onto the request-body key the provider
// reads it from in json_schema mode. The gollm option map is copied into the
// body verbatim, so the key name is the whole contract. An empty key means the
// provider has no structured-output channel and the schema is not sent.
func structuredOutputOption(provider, name string, schema map[string]any) (string, any) {
	switch provider {
	case "openai":
		return "response_format", map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   name,
				"strict": true,
				"schema": schema,
			},
		}
	case "ollama":
		return "format", schema
	default:
		return "", nil
	}
}

// jsonObjectOption is the json_object mode counterpart of
// structuredOutputOption: the key asking for any JSON object.
func jsonObjectOption(provider string) (string, any) {
	switch provider {
	case "openai":
		return "response_format", map[string]any{"type": "json_object"}
	case "ollama":
		return "format", "json"
	default:
		return "", nil
	}
}

// needsAPIKey reports whether the provider requires an API key configured
// in APIKeys. Local providers (ollama) do not.
func needsAPIKey(provider string) bool {
	switch provider {
	case "ollama":
		return false
	default:
		return true
	}
}
