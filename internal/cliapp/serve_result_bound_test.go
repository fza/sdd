package cliapp_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// sdd serve reads Claude Code's MCP output limit from MAX_MCP_OUTPUT_TOKENS
// and bounds a Claude Code caller's results at 2 characters per token of it:
// a read past that bound comes in pages larger than another host's, within
// the lowered bound.
func TestServeBoundsClaudeCodeByItsOutputLimit(t *testing.T) {
	t.Setenv("MAX_MCP_OUTPUT_TOKENS", "15000")
	f := newAppFixture(t)
	f.client = "claude-code"
	var body strings.Builder
	for i := range 600 {
		fmt.Fprintf(&body, "Paragraph %d of the large entry observes how a read pages under the host's bound.\n\n", i)
	}
	const id = "20260914-020001-s-tac-big"
	f.write(t, "decisions/2026/09/14-020001-s-tac-big.md", "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\nparticipants: [Local Author]\nsummary: A large entry.\n---\n\n"+body.String())

	var largest int
	f.serve(t, func(client *mcp.ClientSession) {
		door, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
		if err != nil || door.IsError {
			t.Fatalf("start_session: %v %+v", err, door)
		}
		session, _ := door.StructuredContent.(map[string]any)["session"].(string)
		for page := 1; ; page++ {
			args := map[string]any{"session": session, "ids": []string{id}}
			if page > 1 {
				args["page"] = page
			}
			shown, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "show", Arguments: args})
			if err != nil || shown.IsError {
				t.Fatalf("show page %d: %v %+v", page, err, shown)
			}
			encoded, err := json.Marshal(shown.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			largest = max(largest, len(encoded))
			if !strings.Contains(string(encoded), "for the next;") {
				return
			}
		}
	})
	if largest > 30000 || largest <= 18000 {
		t.Fatalf("a Claude Code caller's pages should fill the bound its output limit sets, the largest took %d characters", largest)
	}
}

// A value of MAX_MCP_OUTPUT_TOKENS that is no count of tokens stops the
// server before it serves.
func TestServeRefusesAnOutputLimitThatIsNoCount(t *testing.T) {
	for _, value := range []string{"many", "-5"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("MAX_MCP_OUTPUT_TOKENS", value)
			f := newAppFixture(t)
			result := f.run(t, "serve", "--transport", "stdio")
			if result.err == nil || !strings.Contains(result.err.Error(), "MAX_MCP_OUTPUT_TOKENS") && !strings.Contains(result.err.Error(), "mcp-output-tokens") {
				t.Fatalf("serve should refuse the value naming the setting, got %v", result.err)
			}
		})
	}
}
