package factory

import (
	"cmp"
	"fmt"
	"time"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
	"github.com/networkteam/sdd/pkg/llm/relevance/cohere"
	"github.com/networkteam/sdd/pkg/llm/relevance/systemone"
)

// Prefilter defaults: a call is bounded by the pass deadline, and the chat
// bridge fans one call per candidate out to the calls in flight.
const (
	defaultPrefilterTimeout     = 45 * time.Second
	defaultPrefilterConcurrency = 16
)

// Prefilter is a composed relevance scorer and the judge it scores as, the
// identity calibration keys its labels on.
type Prefilter struct {
	Scorer relevance.Scorer
	Judge  string
}

// NewPrefilter composes a resolved llm_prefilter block
// (model.BaseConfig.ResolvedPrefilter) into a bounded, observed scorer: a
// dedicated provider's adapter, judge <provider>:<model>, or the generate
// chat bridge over a chat provider's model, judge generate:<model>.
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
		scorer, judge = client, cfg.Provider+":"+cmp.Or(cfg.Model, systemone.DefaultModel)
	case model.PrefilterCohere:
		key, err := prefilterKey(cfg)
		if err != nil {
			return Prefilter{}, err
		}
		client, err := cohere.New(cohere.Options{Endpoint: cfg.Endpoint, Model: cfg.Model, APIKey: key})
		if err != nil {
			return Prefilter{}, fmt.Errorf("llm_prefilter: %w", err)
		}
		scorer, judge = client, cfg.Provider+":"+cmp.Or(cfg.Model, cohere.DefaultModel)
	default:
		err := model.CheckPrefilterProvider(cfg.Provider)
		if err != nil {
			return Prefilter{}, err
		}
		chat, err := New(cfg.LLMConfig)
		if err != nil {
			return Prefilter{}, fmt.Errorf("llm_prefilter: %w", err)
		}
		bridge, err := llmops.NewRelevanceBridge(chat)
		if err != nil {
			return Prefilter{}, err
		}
		concurrency := cfg.Concurrency
		if concurrency <= 0 {
			concurrency = defaultPrefilterConcurrency
		}
		scorer = relevance.PerItem(bridge, concurrency)
		judge = "generate:" + cmp.Or(cfg.Model, cfg.Provider, model.DefaultLLMModel)
	}
	if cfg.RateLimitRPS > 0 {
		scorer = relevance.RateLimited(scorer, cfg.RateLimitRPS)
	}
	scorer = relevance.Observed(relevance.Bounded(scorer, timeout), sink)
	return Prefilter{Scorer: scorer, Judge: judge}, nil
}

// prefilterKey is the API key a dedicated provider authenticates with, keyed
// by the provider as llm.api_keys is.
func prefilterKey(cfg model.LLMPrefilterConfig) (string, error) {
	key := cfg.APIKeys[cfg.Provider]
	if key == "" {
		return "", fmt.Errorf("llm_prefilter: provider %s needs llm_prefilter.api_keys.%s", cfg.Provider, cfg.Provider)
	}
	return key, nil
}
