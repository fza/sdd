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

// Prefilter defaults: a call's own limit, and the judge holds a few calls in
// flight, each carrying a whole batch.
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
// (model.BaseConfig.ResolvedPrefilter) into a bounded, observed scorer: the
// chat judge over the block's model, scoring a batch per call, judge
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
	chat, err := New(cfg.LLMConfig)
	if err != nil {
		return Prefilter{}, fmt.Errorf("llm_prefilter: %w", err)
	}
	judge, err := llmops.NewChatJudge(chat)
	if err != nil {
		return Prefilter{}, err
	}
	scorer := relevance.Limited(judge, cmp.Or(cfg.Concurrency, defaultChatConcurrency))
	if cfg.RateLimitRPS > 0 {
		scorer = relevance.RateLimited(scorer, cfg.RateLimitRPS)
	}
	scorer = relevance.Observed(relevance.Bounded(scorer, timeout), sink)
	modelName := cmp.Or(cfg.Model, cfg.Provider, model.DefaultLLMModel)
	return Prefilter{Scorer: scorer, Judge: "chat:" + modelName}, nil
}
