package cliapp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/test"
)

func TestDryRunPreflightAppendsToTheUsageLog(t *testing.T) {
	f := newAppFixture(t)
	server := test.NewOpenAIServer(t, test.OpenAIBehaviour{
		Answer: `{"findings":[]}`, FinishReason: "stop", PromptTokens: 1200, ReasoningTokens: 300,
		BillingCost: `{"energy":"0.103","paid_with":"energy"}`,
	})
	f.write(t, ".sdd/config.local.yaml", "participant: Local Author\nllm:\n  provider: openai\n  model: glm-5.3-flash\n  endpoint: "+server.URL+"\n  api_keys:\n    openai: op-notarealkeyaaaaaaaaaa\n  retry:\n    max_attempts: 1\n")
	usageLog := filepath.Join(t.TempDir(), "usage.jsonl")
	t.Setenv("SDD_USAGE_LOG", usageLog)

	result := f.run(t, "new", "--confidence", "high", "--dry-run", "s", "tac", "A dry-run signal.")
	if result.err != nil {
		t.Fatalf("new --dry-run: %v\n%s", result.err, result.stderr)
	}

	raw, err := os.ReadFile(usageLog)
	if err != nil {
		t.Fatalf("the pre-flight call should write the usage log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var line struct {
		Step            string          `json:"step"`
		EndpointHost    string          `json:"endpoint_host"`
		PromptTokens    int             `json:"prompt_tokens"`
		ReasoningTokens int             `json:"reasoning_tokens"`
		BillingCost     json.RawMessage `json:"billing_cost"`
		OK              bool            `json:"ok"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &line); err != nil {
		t.Fatal(err)
	}
	if len(lines) != len(server.Requests()) || line.Step != "preflight" || !line.OK {
		t.Fatalf("each pre-flight call should append one line, got %d lines for %d calls: %s", len(lines), len(server.Requests()), raw)
	}
	if line.EndpointHost != "127.0.0.1" || line.PromptTokens != 1200 || line.ReasoningTokens != 300 || string(line.BillingCost) != `{"energy":"0.103","paid_with":"energy"}` {
		t.Errorf("the line should carry what the provider metered, got %s", lines[0])
	}
	if strings.Contains(string(raw), "op-notarealkey") || strings.Contains(string(raw), "dry-run signal") {
		t.Errorf("the usage log should never carry a key or a prompt, got %s", raw)
	}
}
