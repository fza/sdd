package model_test

import (
	"strings"
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

// A choice put to the person goes through the question tool of the agent the
// client name names, and through no tool sdd guesses for any other. Claude
// Desktop reports claude-ai and offers AskUserQuestion in chat.
func TestQuestionTool_NamesTheClientsTool(t *testing.T) {
	for client, want := range map[string]string{
		"claude-code":      "AskUserQuestion",
		"claude-ai":        "AskUserQuestion",
		"Claude Desktop":   "AskUserQuestion",
		"codex-mcp-client": "request_user_input",
		"cursor":           "",
		"":                 "",
	} {
		t.Run(client, func(t *testing.T) {
			got := model.QuestionTool(client)
			if got == "" {
				t.Fatal("every client should be told how to put a choice to its person")
			}
			named := strings.Contains(got, "AskUserQuestion") || strings.Contains(got, "request_user_input")
			if want == "" && named || want != "" && !strings.Contains(got, want) {
				t.Errorf("client %q should be told %q, got %q", client, want, got)
			}
		})
	}
}
