package factory

import (
	"cmp"
	"fmt"
	"time"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
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
// (model.BaseConfig.ResolvedPrefilter) into a bounded, observed scorer: the
// generate chat bridge over the block's model, judge generate:<model>.
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
	chat, err := New(cfg.LLMConfig)
	if err != nil {
		return Prefilter{}, fmt.Errorf("llm_prefilter: %w", err)
	}
	bridge, err := llmops.NewRelevanceBridge(chat)
	if err != nil {
		return Prefilter{}, err
	}
	scorer := relevance.PerItem(bridge, concurrency)
	if cfg.RateLimitRPS > 0 {
		scorer = relevance.RateLimited(scorer, cfg.RateLimitRPS)
	}
	scorer = relevance.Observed(relevance.Bounded(scorer, timeout), sink)
	modelName := cmp.Or(cfg.Model, cfg.Provider, model.DefaultLLMModel)
	return Prefilter{Scorer: scorer, Judge: "generate:" + modelName}, nil
}
