package cliapp_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// callTool calls a tool and returns its serve, its text and structured
// content joined, and whether the call failed.
func callTool(t *testing.T, client *mcp.ClientSession, tool string, args map[string]any) (mcpserver.ServeResult, string, bool) {
	t.Helper()
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var serve mcpserver.ServeResult
	_ = json.Unmarshal(encoded, &serve)
	var text strings.Builder
	for _, content := range result.Content {
		textContent, ok := content.(*mcp.TextContent)
		if ok {
			text.WriteString(textContent.Text)
		}
	}
	return serve, text.String() + string(encoded), result.IsError
}

func captureReport() map[string]any {
	return map[string]any{
		"body":        "The platform graph lacks a record of how the home project consumes its API, so platform changes break the home project unnoticed.",
		"entryKind":   "gap",
		"layer":       "tactical",
		"topics":      []string{"testing/connected"},
		"confidence":  "low",
		"widenReport": "searched the platform graph for consumer records; it holds none.",
	}
}
