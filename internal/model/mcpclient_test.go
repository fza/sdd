package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// Claude Code, interactive and as claude -p, reports the client name
// claude-code; Codex reports codex-mcp-client.
func TestMCPClients_MatchTheNameCaseInsensitively(t *testing.T) {
	for client, want := range map[string]struct{ claude, codex bool }{
		"claude-code":      {claude: true},
		"Claude Desktop":   {claude: true},
		"codex-mcp-client": {codex: true},
		"CODEX":            {codex: true},
		"cursor":           {},
		"":                 {},
	} {
		t.Run(client, func(t *testing.T) {
			if model.ClaudeClient(client) != want.claude || model.CodexClient(client) != want.codex {
				t.Fatalf("client %q should be classified by the agent its name names", client)
			}
		})
	}
}
