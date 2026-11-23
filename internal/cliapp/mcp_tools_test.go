package cliapp_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// answered is args with the text question's answer a tool that advances a
// session requires, lost when they carry none: these tests do not hold the
// texts asked about.
func answered(tool string, args map[string]any) map[string]any {
	switch tool {
	case "start_procedure", "next", "park", "abandon":
	default:
		return args
	}
	if _, ok := args["answer"]; ok {
		return args
	}
	out := make(map[string]any, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	out["answer"] = "lost"
	return out
}

// callTool calls a tool and returns its serve, its text and structured
// content joined, and whether the call failed.
func callTool(t *testing.T, client *mcp.ClientSession, tool string, args map[string]any) (mcpserver.ServeResult, string, bool) {
	t.Helper()
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: answered(tool, args)})
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
