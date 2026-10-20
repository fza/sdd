package llmstats

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/pkg/llm"
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

func NewUsageLog(path string) *UsageLog {
	return &UsageLog{path: path, now: time.Now}
}

// RecordCall appends the call's line. A failed write is dropped with a debug
// log line: metering must never fail the call it meters.
func (l *UsageLog) RecordCall(ctx context.Context, stat llm.CallStat) {
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
		slogutils.FromContext(ctx).Debug("encoding usage log line", "error", err)
		return
	}

	file, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		slogutils.FromContext(ctx).Debug("opening usage log", "path", l.path, "error", err)
		return
	}
	defer file.Close()
	_, err = file.Write(append(line, '\n'))
	if err != nil {
		slogutils.FromContext(ctx).Debug("writing usage log", "path", l.path, "error", err)
	}
}
