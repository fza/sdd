package cliapp_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
)

func TestServeAndInfoOnASchemaOneGraph(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/meta.json", "{\n  \"graph_schema_version\": 1\n}\n")

	info := f.run(t, "info")
	if info.err != nil {
		t.Fatalf("info: %v\n%s", info.err, info.stderr)
	}
	assertContainsOutput(t, "info", info.stdout, "Schema: 1")

	f.serve(t, func(client *mcp.ClientSession) {
		door, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
		if err != nil || door.IsError {
			t.Fatalf("serve should open a session on a schema-1 graph: %v %+v", err, door)
		}
		served, err := json.Marshal(door)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(served), "until `sdd migrate`") {
			t.Errorf("a graph at the binary's schema should owe no migration in the session framing:\n%s", served)
		}
		handle, _ := door.StructuredContent.(map[string]any)["session"].(string)
		result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "info", Arguments: map[string]any{"session": handle}})
		if err != nil || result.IsError {
			t.Fatalf("info: %v %+v", err, result)
		}
		structured, _ := result.StructuredContent.(map[string]any)
		if structured["graphSchema"] != float64(1) || structured["binarySchema"] != float64(model.CurrentGraphSchemaVersion) {
			t.Errorf("MCP info should report both schemas, got %+v", structured)
		}
	})
}
