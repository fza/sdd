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

// Prefilter defaults (20261109-004506-d-cpt-udz, 5.8): a call is bounded by
// the pass deadline, and a one-pair protocol fans out up to the calls in
// flight.
const (
	defaultPrefilterTimeout     = 45 * time.Second
	defaultPrefilterConcurrency = 16
	defaultPrefilterKeyProvider = "opper"
)

// Prefilter is a composed relevance scorer and the judge it scores as: the
// protocol and the model, the identity calibration keys its labels on.
type Prefilter struct {
	Scorer relevance.Scorer
	Judge  string
}

// NewPrefilter composes the llm_prefilter block into a bounded, observed
// scorer.
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
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = defaultPrefilterConcurrency
	}
	var scorer relevance.Scorer
	var modelName string
	switch cfg.Protocol {
	case model.PrefilterSystemOne:
		modelName = cmp.Or(cfg.Model, systemone.DefaultModel)
		provider, key, err := prefilterKey(cfg)
		if err != nil {
			return Prefilter{}, err
		}
		client, err := systemone.New(systemone.Options{Endpoint: cfg.Endpoint, Model: modelName, APIKey: key, Provider: provider})
		if err != nil {
			return Prefilter{}, fmt.Errorf("llm_prefilter: %w", err)
		}
		scorer = client
	case model.PrefilterCohereRerank:
		modelName = cmp.Or(cfg.Model, cohere.DefaultModel)
		provider, key, err := prefilterKey(cfg)
		if err != nil {
			return Prefilter{}, err
		}
		client, err := cohere.New(cohere.Options{Endpoint: cfg.Endpoint, Model: modelName, APIKey: key, Provider: provider})
		if err != nil {
			return Prefilter{}, fmt.Errorf("llm_prefilter: %w", err)
		}
		scorer = client
	case model.PrefilterGenerate:
		chat, err := New(cfg.LLMConfig)
		if err != nil {
			return Prefilter{}, fmt.Errorf("llm_prefilter: %w", err)
		}
		bridge, err := llmops.NewRelevanceBridge(chat)
		if err != nil {
			return Prefilter{}, err
		}
		modelName = cmp.Or(cfg.ResolvedModel(), cfg.ResolvedProvider())
		scorer = relevance.PerItem(bridge, concurrency)
	case "":
		return Prefilter{}, fmt.Errorf("verify.prefilter is on but llm_prefilter.protocol is not set (supported: %s, %s, %s)", model.PrefilterSystemOne, model.PrefilterCohereRerank, model.PrefilterGenerate)
	default:
		return Prefilter{}, fmt.Errorf("unknown llm_prefilter.protocol %q (supported: %s, %s, %s)", cfg.Protocol, model.PrefilterSystemOne, model.PrefilterCohereRerank, model.PrefilterGenerate)
	}
	if cfg.RateLimitRPS > 0 {
		scorer = relevance.RateLimited(scorer, cfg.RateLimitRPS)
	}
	scorer = relevance.Observed(relevance.Bounded(scorer, timeout), sink)
	return Prefilter{Scorer: scorer, Judge: cfg.Protocol + ":" + modelName}, nil
}

// prefilterKey returns the key provider a direct protocol authenticates with,
// opper unless llm_prefilter.provider names another, and its API key.
func prefilterKey(cfg model.LLMPrefilterConfig) (string, string, error) {
	provider := cmp.Or(cfg.Provider, defaultPrefilterKeyProvider)
	key := cfg.APIKeys[provider]
	if key == "" {
		return "", "", fmt.Errorf("llm_prefilter: protocol %s needs llm_prefilter.api_keys.%s", cfg.Protocol, provider)
	}
	return provider, key, nil
}
