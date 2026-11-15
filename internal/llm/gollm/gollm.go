// Package gollm implements llm.Runner on top of github.com/teilomillet/gollm,
// providing a unified adapter for Anthropic API, OpenAI, Ollama, and other
// providers supported by gollm. This is the opt-in alternative to the
// claude-cli bridge; selected via the llm.provider config setting.
package gollm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	upstream "github.com/teilomillet/gollm"
	upstreamllm "github.com/teilomillet/gollm/llm"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

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
	useCache bool

	// ollama sizes num_ctx per call: Ollama cuts a prompt longer than its
	// context without an error. gollm reads options from the client on every
	// attempt, so each num_ctx gets a client of its own and calls of one size
	// run side by side.
	ollama        bool
	ollamaMu      sync.Mutex
	ollamaClients map[int]upstream.LLM
	newClient     func() (upstream.LLM, error)
}

// NewRunner constructs a gollm-backed Runner from an LLMConfig. Provider
// must be one of the gollm-supported providers (e.g. "anthropic", "openai",
// "ollama"). Returns a typed error when required config is missing (API
// key for remote providers).
func NewRunner(cfg model.LLMConfig) (*Runner, error) {
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

	opts := []upstream.ConfigOption{
		upstream.SetProvider(cfg.Provider),
		upstream.SetModel(cfg.Model),
		upstream.SetMaxRetries(maxAttempts - 1),
		upstream.SetRetryDelay(baseDelay),
		upstream.SetMaxRetryDelay(maxDelay),
		// Gollm's default MaxTokens (256) truncates longer summaries and
		// pre-flight JSON responses mid-sentence. 4096 covers summaries
		// (~150 tokens), pre-flight findings arrays, and has headroom for
		// richer prompts without being wasteful.
		upstream.SetMaxTokens(4096),
	}

	// API key is required for remote providers.
	if needsAPIKey(cfg.Provider) {
		key := cfg.APIKeys[cfg.Provider]
		if key == "" {
			return nil, fmt.Errorf("gollm: api key missing for provider %q — run `sdd config set llm.api_keys.%s <key>`", cfg.Provider, cfg.Provider)
		}
		opts = append(opts, upstream.SetAPIKey(key))
	}

	// Ollama endpoint override. A key is optional: local deployments run
	// without one, Ollama Cloud (ollama.com) authenticates with it as a
	// Bearer token.
	if cfg.Provider == "ollama" {
		if cfg.OllamaEndpoint != "" {
			opts = append(opts, upstream.SetOllamaEndpoint(cfg.OllamaEndpoint))
		}
		if key := cfg.APIKeys["ollama"]; key != "" {
			opts = append(opts, upstream.SetAPIKey(key))
		}
	}

	if cfg.Endpoint != "" {
		switch cfg.Provider {
		case "openai":
			opts = append(opts, upstream.SetOpenAIEndpoint(cfg.Endpoint))
		case "anthropic":
			opts = append(opts, upstream.SetAnthropicEndpoint(cfg.Endpoint))
		}
	}

	// Enable Anthropic prompt caching — sends the anthropic-beta header so
	// cache_control blocks on system prompts are honored server-side.
	useCache := cfg.Provider == "anthropic"
	if useCache {
		opts = append(opts, upstream.SetEnableCaching(true))
	}

	// Timeout flows into gollm's HTTP client.
	if cfg.Timeout != "" {
		d, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return nil, fmt.Errorf("gollm: parsing timeout %q: %w", cfg.Timeout, err)
		}
		opts = append(opts, upstream.SetTimeout(d))
	}

	newClient := func() (upstream.LLM, error) {
		client, err := upstream.NewLLM(opts...)
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
		return client, nil
	}
	client, err := newClient()
	if err != nil {
		return nil, err
	}

	runner := &Runner{
		client:    client,
		provider:  cfg.Provider,
		model:     cfg.Model,
		variant:   canonicalVariant(cfg.Params),
		useCache:  useCache,
		ollama:    cfg.Provider == "ollama",
		newClient: newClient,
	}
	if runner.ollama {
		client.SetOption("options", map[string]any{"num_ctx": ollamaMinContext})
		runner.ollamaClients = map[int]upstream.LLM{ollamaMinContext: client}
	}
	return runner, nil
}

// clientFor returns the client a call of req goes through: for Ollama the
// one carrying its num_ctx, created on first use.
func (r *Runner) clientFor(req llm.Request) (upstream.LLM, error) {
	if !r.ollama {
		return r.client, nil
	}
	size := ollamaContextSize(req)
	r.ollamaMu.Lock()
	defer r.ollamaMu.Unlock()
	if client, ok := r.ollamaClients[size]; ok {
		return client, nil
	}
	client, err := r.newClient()
	if err != nil {
		return nil, err
	}
	client.SetOption("options", map[string]any{"num_ctx": size})
	r.ollamaClients[size] = client
	return client, nil
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
	return llm.Identity{Provider: r.provider, Model: r.model, Variant: r.variant}
}

// Run sends the request to the underlying gollm client. When caching is
// enabled (Anthropic), the SystemPrompt is tagged ephemeral so the server
// caches the prefix. Other providers ignore the cache hint but still see
// the split system/user message pair. A ResponseSchema goes to the provider's
// native structured output. Failures carry the identity through the typed
// llm.Error so they measure like successes.
func (r *Runner) Run(ctx context.Context, req llm.Request) (llm.Result, error) {
	var opts []upstream.PromptOption
	if req.SystemPrompt != "" {
		cacheType := upstream.CacheType("")
		if r.useCache {
			cacheType = upstream.CacheTypeEphemeral
		}
		opts = append(opts, upstream.WithSystemPrompt(req.SystemPrompt, cacheType))
	}

	prompt := upstream.NewPrompt(req.UserPrompt, opts...)

	var genOpts []upstreamllm.GenerateOption
	if len(req.ResponseSchema) > 0 {
		genOpts = append(genOpts, upstream.WithResponseSchema(req.ResponseSchema))
	}

	client, err := r.clientFor(req)
	if err != nil {
		return llm.Result{}, &llm.Error{Identity: r.identity(), Err: err}
	}
	resp, err := client.GenerateWithUsage(ctx, prompt, genOpts...)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("gollm %s: timed out: %w", r.provider, ctx.Err())
		} else {
			err = fmt.Errorf("gollm %s: %w", r.provider, err)
		}
		return llm.Result{}, &llm.Error{Identity: r.identity(), Err: err}
	}

	return llm.Result{Text: resp.Content, Identity: r.identity(), Usage: usageFromResponse(resp.Usage)}, nil
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
		CostUSD:           u.CostUSD,
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

// ollamaOutputReserve is the answer room an Ollama call's num_ctx keeps
// beside its prompt, in tokens.
const ollamaOutputReserve = 8192

// ollamaMinContext is the smallest num_ctx an Ollama call carries.
const ollamaMinContext = 16384

// ollamaContextSize is the num_ctx an Ollama call carries: the prompt at 3
// characters a token plus the answer reserve, rounded up to a power of two.
// Ollama reloads the model when num_ctx changes, so calls of similar size
// share one value.
func ollamaContextSize(req llm.Request) int {
	need := (len(req.SystemPrompt)+len(req.UserPrompt))/3 + 1 + ollamaOutputReserve
	size := ollamaMinContext
	for size < need {
		size *= 2
	}
	return size
}
