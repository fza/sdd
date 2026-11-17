package model

import "strings"

// ClaudeClient reports whether the MCP client name clientName identifies a
// Claude agent: the name holds "claude", matched case-insensitively, so a
// renamed build still matches.
func ClaudeClient(clientName string) bool {
	return strings.Contains(strings.ToLower(clientName), "claude")
}

// CodexClient reports whether clientName identifies Codex: the name holds
// "codex", matched case-insensitively.
func CodexClient(clientName string) bool {
	return strings.Contains(strings.ToLower(clientName), "codex")
}

// QuestionTool says how an agent under the MCP client clientName puts a
// choice to its person: the question tool of Claude Code or Codex, and for
// any other client its host's own, which sdd does not know.
func QuestionTool(clientName string) string {
	switch {
	case strings.Contains(strings.ToLower(clientName), "claude-code"):
		return "the AskUserQuestion tool"
	case CodexClient(clientName):
		return "the request_user_input tool where your tools list it, else one plain-text question"
	}
	return "your host's question tool where it has one, else one plain-text question"
}
