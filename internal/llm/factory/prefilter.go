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

// Prefilter defaults: a call's own limit, which relevance.Halving shortens
// inside a pass so a timeout leaves room for halves, and chat holds a few
// calls in flight, each carrying a whole batch.
const (
	defaultPrefilterTimeout     = 45 * time.Second
	defaultChatConcurrency      = 8
	defaultPrefilterKeyProvider = "opper"
)

// Prefilter is a composed relevance scorer and the judge it scores as: the
// protocol and the model, the identity calibration keys its labels on.
// Revision names the chat prompt's revision, empty for a reranker, which
// reads no prompt; Scan is the scan after writing on the same chat model,
// nil for a reranker, which answers no question.
type Prefilter struct {
	Scorer   relevance.Scorer
	Judge    string
	Revision string
	Scan     *llmops.ScanJudge
}

// NewPrefilter composes a resolved llm_prefilter block
// (model.BaseConfig.ResolvedPrefilter) into a bounded, observed scorer. A
// chat judge starts each request at the batch size sizes holds for it
// (relevance.Halving); nil sizes keep it for the process alone.
func NewPrefilter(cfg model.LLMPrefilterConfig, sink llm.StatsSink, sizes relevance.BatchSizes) (Prefilter, error) {
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
	var scorer relevance.Scorer
	var scan *llmops.ScanJudge
	var revision string
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
	case model.PrefilterCohere:
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
	case model.PrefilterChat:
		chat, err := New(cfg.LLMConfig)
		if err != nil {
			return Prefilter{}, fmt.Errorf("llm_prefilter: %w", err)
		}
		judge, err := llmops.NewChatJudge(chat)
		if err != nil {
			return Prefilter{}, err
		}
		scan, err = llmops.NewScanJudge(llm.Observed(chat, sink))
		if err != nil {
			return Prefilter{}, err
		}
		revision = llmops.ChatRevision()
		modelName = cmp.Or(cfg.ResolvedModel(), cfg.ResolvedProvider())
		scorer = relevance.Limited(judge, cmp.Or(cfg.Concurrency, defaultChatConcurrency))
	case "":
		return Prefilter{}, fmt.Errorf("verify.prefilter is on but llm_prefilter.protocol is not set (supported: %s)", strings.Join(cfg.Protocol.AllowedValues(), ", "))
	default:
		return Prefilter{}, fmt.Errorf("unknown llm_prefilter.protocol %q (supported: %s)", cfg.Protocol, strings.Join(cfg.Protocol.AllowedValues(), ", "))
	}
	if cfg.RateLimitRPS > 0 {
		scorer = relevance.RateLimited(scorer, cfg.RateLimitRPS)
	}
	judge := string(cfg.Protocol) + ":" + modelName
	scorer = relevance.Observed(relevance.Bounded(relevance.RetriedAfterDelay(scorer, timeout), timeout), sink)
	if cfg.Protocol == model.PrefilterChat {
		if sizes == nil {
			sizes = &relevance.MemoryBatchSizes{}
		}
		// Outermost, where relevance.ScoreWithFar finds it.
		scorer = relevance.Halving(scorer, sizes, judge)
	}
	return Prefilter{Scorer: scorer, Judge: judge, Revision: revision, Scan: scan}, nil
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
