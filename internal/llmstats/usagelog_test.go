package llmstats_test

import (
	"bufio"
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/llmstats"
	"github.com/networkteam/sdd/pkg/llm"
)

const meliousBillingCost = `{"energy":"0.103","credits":"0.0000103","paid_with":"energy"}`

func readUsageLog(t *testing.T, path string) []map[string]json.RawMessage {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening usage log: %v", err)
	}
	defer file.Close()
	var lines []map[string]json.RawMessage
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var line map[string]json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("every usage log line should be one JSON object, got %q: %v", scanner.Text(), err)
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

func TestUsageLogRecordsOneLinePerCall(t *testing.T) {
	identity := llm.Identity{Provider: "openai", Model: "glm-5.3-flash", Variant: "reasoning_effort=medium", Host: "api.melious.ai"}
	for _, tc := range []struct {
		name string
		stat llm.CallStat
		want map[string]string
	}{
		{
			name: "answered call",
			stat: llm.CallStat{
				Purpose: "preflight", Identity: identity, FinishReason: "stop",
				Usage: llm.Usage{InputTokens: 19, OutputTokens: 21, ReasoningTokens: 19, CacheReadTokens: 5, BillingCost: meliousBillingCost},
			},
			want: map[string]string{
				"step": `"preflight"`, "provider": `"openai"`, "endpoint_host": `"api.melious.ai"`, "model": `"glm-5.3-flash"`,
				"prompt_tokens": "19", "completion_tokens": "21", "reasoning_tokens": "19", "cached_tokens": "5",
				"finish_reason": `"stop"`, "billing_cost": meliousBillingCost, "ok": "true",
			},
		},
		{
			name: "billed reply that failed",
			stat: llm.CallStat{
				Purpose: "preflight-extract", Identity: identity, FinishReason: "length", Error: "answer cut at the token cap",
				Usage: llm.Usage{InputTokens: 900, OutputTokens: 4096, ReasoningTokens: 4096, BillingCost: meliousBillingCost},
			},
			want: map[string]string{
				"step": `"preflight-extract"`, "provider": `"openai"`, "endpoint_host": `"api.melious.ai"`, "model": `"glm-5.3-flash"`,
				"prompt_tokens": "900", "completion_tokens": "4096", "reasoning_tokens": "4096", "cached_tokens": "0",
				"finish_reason": `"length"`, "billing_cost": meliousBillingCost, "ok": "false",
			},
		},
		{
			name: "unattributed failure without billing",
			stat: llm.CallStat{Purpose: "summarize", Error: "transport down"},
			want: map[string]string{
				"step": `"summarize"`, "provider": `""`, "endpoint_host": `""`, "model": `""`,
				"prompt_tokens": "0", "completion_tokens": "0", "reasoning_tokens": "0", "cached_tokens": "0",
				"finish_reason": `""`, "ok": "false",
			},
		},
		{
			name: "billing cost that is not JSON",
			stat: llm.CallStat{Purpose: "embed-query", Identity: identity, Usage: llm.Usage{InputTokens: 7, BillingCost: "{energy"}},
			want: map[string]string{
				"step": `"embed-query"`, "provider": `"openai"`, "endpoint_host": `"api.melious.ai"`, "model": `"glm-5.3-flash"`,
				"prompt_tokens": "7", "completion_tokens": "0", "reasoning_tokens": "0", "cached_tokens": "0",
				"finish_reason": `""`, "ok": "true",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "usage.jsonl")
			before := time.Now().UTC().Truncate(time.Second)

			llmstats.NewUsageLog(path).RecordCall(context.Background(), tc.stat)

			lines := readUsageLog(t, path)
			if len(lines) != 1 {
				t.Fatalf("one call should append one line, got %d", len(lines))
			}
			line := lines[0]
			var at string
			if err := json.Unmarshal(line["at"], &at); err != nil {
				t.Fatalf("at should be a string: %v", err)
			}
			stamp, err := time.Parse(time.RFC3339, at)
			if err != nil || !strings.HasSuffix(at, "Z") || stamp.Before(before) {
				t.Errorf("at should be the call's RFC 3339 UTC time, got %q", at)
			}
			delete(line, "at")
			got := map[string]string{}
			for key, value := range line {
				got[key] = string(value)
			}
			if !maps.Equal(got, tc.want) {
				t.Errorf("the line should carry exactly the contracted fields\n got: %v\nwant: %v", got, tc.want)
			}
		})
	}
}

func TestUsageLogAppendsToAnExistingLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if err := os.WriteFile(path, []byte(`{"step":"earlier"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	llmstats.NewUsageLog(path).RecordCall(context.Background(), llm.CallStat{Purpose: "preflight"})

	lines := readUsageLog(t, path)
	if len(lines) != 2 || string(lines[0]["step"]) != `"earlier"` {
		t.Fatalf("existing lines should be kept and the new one appended, got %v", lines)
	}
}

func TestUsageLogConcurrentWritersNeverInterleaveALine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	writers := []*llmstats.UsageLog{llmstats.NewUsageLog(path), llmstats.NewUsageLog(path)}
	const callsPerWriter = 200
	stat := llm.CallStat{
		Purpose: "preflight", Identity: llm.Identity{Provider: "openai", Model: strings.Repeat("m", 512), Host: "api.melious.ai"},
		Usage: llm.Usage{InputTokens: 1, BillingCost: meliousBillingCost},
	}

	var wait sync.WaitGroup
	for _, writer := range writers {
		for range callsPerWriter {
			wait.Go(func() { writer.RecordCall(context.Background(), stat) })
		}
	}
	wait.Wait()

	if lines := readUsageLog(t, path); len(lines) != len(writers)*callsPerWriter {
		t.Fatalf("every call should land as its own whole line, got %d", len(lines))
	}
}

func TestUsageLogUnwritablePathKeepsTheCallRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "usage.jsonl")

	llmstats.NewUsageLog(path).RecordCall(context.Background(), llm.CallStat{Purpose: "preflight"})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("an unwritable usage log should be skipped, got %v", err)
	}
}

type purposeSink struct {
	mu       sync.Mutex
	purposes []string
}

func (s *purposeSink) RecordCall(_ context.Context, stat llm.CallStat) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purposes = append(s.purposes, stat.Purpose)
}

func TestFanoutHandsEveryCallToEachSink(t *testing.T) {
	first, second := &purposeSink{}, &purposeSink{}

	llmstats.Fanout(first, second).RecordCall(context.Background(), llm.CallStat{Purpose: "preflight"})
	llmstats.Fanout().RecordCall(context.Background(), llm.CallStat{Purpose: "ignored"})

	if !slices.Equal(first.purposes, []string{"preflight"}) || !slices.Equal(second.purposes, []string{"preflight"}) {
		t.Fatalf("each sink should receive the call once, got %v and %v", first.purposes, second.purposes)
	}
}
