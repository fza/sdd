package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// Claude Code, interactive and as claude -p, reports the client name
// claude-code; Codex reports codex-mcp-client.
func TestClaudeClient_MatchesANameHoldingClaude(t *testing.T) {
	for client, want := range map[string]bool{
		"claude-code":      true,
		"CLAUDE-CODE":      true,
		"Claude Desktop":   true,
		"claude-ai":        true,
		"codex-mcp-client": false,
		"cursor":           false,
		"":                 false,
	} {
		t.Run(client, func(t *testing.T) {
			if got := model.ClaudeClient(client); got != want {
				t.Fatalf("client %q should be classified as a Claude agent only when its name names Claude, got %v", client, got)
			}
		})
	}
}
