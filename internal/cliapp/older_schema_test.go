package cliapp_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
)

func TestServeAndInfoOnAnOlderGraph(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/meta.json", "{\n  \"graph_schema_version\": 1\n}\n")

	info := f.run(t, "info")
	if info.err != nil {
		t.Fatalf("info: %v\n%s", info.err, info.stderr)
	}
	assertContainsOutput(t, "info", info.stdout, fmt.Sprintf("Schema: graph 1, binary %d (writes keep schema 1 until `sdd migrate`)", model.CurrentGraphSchemaVersion))

	f.serve(t, func(client *mcp.ClientSession) {
		door, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
		if err != nil || door.IsError {
			t.Fatalf("serve should open a session on an older graph: %v %+v", err, door)
		}
		served, err := json.Marshal(door)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(served), "Graph schema: 1") {
			t.Errorf("the session framing should name the graph's older schema:\n%s", served)
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
