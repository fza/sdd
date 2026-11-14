package model

import "strings"

// ClaudeClient reports whether the MCP client name clientName identifies a
// Claude agent: the name holds "claude", matched case-insensitively, so a
// renamed build still matches.
func ClaudeClient(clientName string) bool {
	return strings.Contains(strings.ToLower(clientName), "claude")
}
