package llmstats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/embed"
)

// UsageLogEnv names the environment variable holding the path of the usage
// log; unset, no usage log is written.
const UsageLogEnv = "SDD_USAGE_LOG"

// UsageRecord is one line of the usage log: what a caller outside sdd needs to
// meter a call. Token counts are the provider's own, unadjusted, so whether
// PromptTokens includes CachedTokens follows the provider's convention. It
// never carries a key, a prompt or an answer.
type UsageRecord struct {
	At               string          `json:"at"`
	Step             string          `json:"step"`
	Provider         string          `json:"provider"`
	EndpointHost     string          `json:"endpoint_host"`
	Model            string          `json:"model"`
	PromptTokens     int             `json:"prompt_tokens"`
	CompletionTokens int             `json:"completion_tokens"`
	ReasoningTokens  int             `json:"reasoning_tokens"`
	CachedTokens     int             `json:"cached_tokens"`
	FinishReason     string          `json:"finish_reason"`
	BillingCost      json.RawMessage `json:"billing_cost,omitempty"`
	OK               bool            `json:"ok"`
}

// UsageLog appends one UsageRecord per LLM call to a file the caller names,
// so a host running sdd can meter what sdd spends. Each line goes out in a
// single write to a file opened with O_APPEND, so concurrent sdd processes
// never interleave a line.
type UsageLog struct {
	path string
	now  func() time.Time
}

// NewUsageLog creates the file at path if missing, so an unwritable path fails
// before the first call.
func NewUsageLog(path string) (*UsageLog, error) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening usage log: %w", err)
	}
	err = file.Close()
	if err != nil {
		return nil, fmt.Errorf("closing usage log: %w", err)
	}
	return &UsageLog{path: path, now: time.Now}, nil
}

// Runner decorates r so every call appends its line; a line that cannot be
// written fails the call. A nil log returns r unwrapped.
func (l *UsageLog) Runner(r llm.Runner) llm.Runner {
	if l == nil {
		return r
	}
	return llm.RunnerFunc(func(ctx context.Context, req llm.Request) (llm.Result, error) {
		var call callCapture
		result, err := llm.Observed(r, &call).Run(ctx, req)
		return result, l.appendFor(err, call.stat)
	})
}

// Embedder decorates e as Runner decorates a runner.
func (l *UsageLog) Embedder(e embed.Embedder) embed.Embedder {
	if l == nil {
		return e
	}
	return usageLoggedEmbedder{inner: e, log: l}
}

type usageLoggedEmbedder struct {
	inner embed.Embedder
	log   *UsageLog
}

func (e usageLoggedEmbedder) Embed(ctx context.Context, req embed.Request) (embed.Result, error) {
	var call callCapture
	result, err := embed.Observed(e.inner, &call).Embed(ctx, req)
	return result, e.log.appendFor(err, call.stat)
}

func (e usageLoggedEmbedder) Fingerprint() string {
	return e.inner.Fingerprint()
}

// callCapture keeps the CallStat the observing decorators build, so the usage
// log meters a call exactly as the stats sink does.
type callCapture struct{ stat llm.CallStat }

func (c *callCapture) RecordCall(_ context.Context, stat llm.CallStat) {
	c.stat = stat
}

func (l *UsageLog) appendFor(callErr error, stat llm.CallStat) error {
	err := l.Append(stat)
	if err != nil {
		return errors.Join(callErr, err)
	}
	return callErr
}

// Append writes the call's line.
func (l *UsageLog) Append(stat llm.CallStat) error {
	record := UsageRecord{
		At:               l.now().UTC().Format(time.RFC3339),
		Step:             stat.Purpose,
		Provider:         stat.Identity.Provider,
		EndpointHost:     stat.Identity.Host,
		Model:            stat.Identity.Model,
		PromptTokens:     stat.Usage.InputTokens,
		CompletionTokens: stat.Usage.OutputTokens,
		ReasoningTokens:  stat.Usage.ReasoningTokens,
		CachedTokens:     stat.Usage.CacheReadTokens,
		FinishReason:     stat.FinishReason,
		OK:               stat.Error == "",
	}
	if json.Valid([]byte(stat.Usage.BillingCost)) {
		record.BillingCost = json.RawMessage(stat.Usage.BillingCost)
	}
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encoding usage log line: %w", err)
	}

	file, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening usage log: %w", err)
	}
	defer file.Close()
	_, err = file.Write(append(line, '\n'))
	if err != nil {
		return fmt.Errorf("writing usage log: %w", err)
	}
	return nil
}
