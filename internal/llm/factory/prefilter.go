package factory

import (
	"cmp"
	"fmt"
	"strings"
	"time"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
	"github.com/networkteam/sdd/pkg/llm/relevance/cohere"
	"github.com/networkteam/sdd/pkg/llm/relevance/systemone"
)

// Prefilter defaults: a call's own limit, and the chat judge holds a few
// calls in flight, each carrying a whole batch.
const (
	defaultPrefilterTimeout = 45 * time.Second
	defaultChatConcurrency  = 8
)

// Prefilter is a composed relevance scorer and the judge it scores as, the
// identity calibration keys its labels on.
type Prefilter struct {
	Scorer relevance.Scorer
	Judge  string
}

// NewPrefilter composes a resolved llm_prefilter block
// (model.BaseConfig.ResolvedPrefilter) into a bounded, observed scorer: a
// dedicated provider's adapter, judge <provider>:<model>, or the chat judge
// over a chat provider's model, scoring a batch per call, judge
// chat:<model>.
func NewPrefilter(cfg model.LLMPrefilterConfig, sink llm.StatsSink) (Prefilter, error) {
	timeout := defaultPrefilterTimeout
	if cfg.Timeout != "" {
		d, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return Prefilter{}, fmt.Errorf("llm_prefilter: parsing timeout %q: %w", cfg.Timeout, err)
		}
		if d > 0 {
			timeout = d
		}
	}
	if cfg.Provider.Dedicated() {
		ignored := chatOnlyKeys(cfg)
		if len(ignored) > 0 {
			return Prefilter{}, fmt.Errorf("llm_prefilter: provider %s takes no %s: chat providers only", cfg.Provider, strings.Join(ignored, ", "))
		}
	}
	var (
		scorer relevance.Scorer
		judge  string
	)
	switch cfg.Provider {
	case model.PrefilterSystemOne:
		key, err := prefilterKey(cfg)
		if err != nil {
			return Prefilter{}, err
		}
		client, err := systemone.New(systemone.Options{Endpoint: cfg.Endpoint, Model: cfg.Model, APIKey: key})
		if err != nil {
			return Prefilter{}, fmt.Errorf("llm_prefilter: %w", err)
		}
		scorer, judge = client, string(cfg.Provider)+":"+cmp.Or(cfg.Model, systemone.DefaultModel)
	case model.PrefilterCohere:
		key, err := prefilterKey(cfg)
		if err != nil {
			return Prefilter{}, err
		}
		client, err := cohere.New(cohere.Options{Endpoint: cfg.Endpoint, Model: cfg.Model, APIKey: key})
		if err != nil {
			return Prefilter{}, fmt.Errorf("llm_prefilter: %w", err)
		}
		scorer, judge = client, string(cfg.Provider)+":"+cmp.Or(cfg.Model, cohere.DefaultModel)
	default:
		chat, err := New(cfg.Chat())
		if err != nil {
			return Prefilter{}, fmt.Errorf("llm_prefilter: %w", err)
		}
		chatJudge, err := llmops.NewChatJudge(chat)
		if err != nil {
			return Prefilter{}, err
		}
		scorer = relevance.Limited(chatJudge, cmp.Or(cfg.Concurrency, defaultChatConcurrency))
		judge = "chat:" + cmp.Or(cfg.Model, string(cfg.Provider), model.DefaultLLMModel)
	}
	if cfg.RateLimitRPS > 0 {
		scorer = relevance.RateLimited(scorer, cfg.RateLimitRPS)
	}
	scorer = relevance.Observed(relevance.Bounded(relevance.RetriedAfterDelay(scorer, timeout), timeout), sink)
	return Prefilter{Scorer: scorer, Judge: judge}, nil
}

// prefilterKey is the API key a dedicated provider authenticates with, keyed
// by the provider as llm.api_keys is.
func prefilterKey(cfg model.LLMPrefilterConfig) (string, error) {
	key := cfg.APIKeys[string(cfg.Provider)]
	if key == "" {
		return "", fmt.Errorf("llm_prefilter: provider %s needs llm_prefilter.api_keys.%s", cfg.Provider, cfg.Provider)
	}
	return key, nil
}

// chatOnlyKeys lists the keys cfg sets that only a chat provider's judge
// reads.
func chatOnlyKeys(cfg model.LLMPrefilterConfig) []string {
	var keys []string
	if cfg.Concurrency != 0 {
		keys = append(keys, "concurrency")
	}
	if cfg.OllamaEndpoint != "" {
		keys = append(keys, "ollama_endpoint")
	}
	if len(cfg.Params) > 0 {
		keys = append(keys, "params")
	}
	if cfg.Retry != (model.RetryConfig{}) {
		keys = append(keys, "retry")
	}
	return keys
}
