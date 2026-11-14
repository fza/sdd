package cliapp_test

import (
	"encoding/json"
	"path/filepath"
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
		if strings.Contains(string(served), "entries are written at schema") {
			t.Errorf("a graph at the binary's schema should get no older-schema line from start_session:\n%s", served)
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

func TestWritesOnASchemaOneGraphCarryTitles(t *testing.T) {
	project := newUntitledProject(t)

	output, err := runReleased(t, project, "new", "s", "tac", "--confidence", "high", "--skip-preflight", "--summary", "S.", "A new gap.")
	if err != nil {
		t.Fatalf("a write on a schema-1 graph should go through: %v\n%s", err, output)
	}
	written := gitIn(t, project, "show", "--name-only", "--format=", "HEAD")
	entry := readFile(t, filepath.Join(project, strings.TrimSpace(written)))
	if !strings.Contains(entry, "\ntitle: ") {
		t.Errorf("an entry on a schema-1 graph should carry a generated title:\n%s", entry)
	}
	if meta := readFile(t, filepath.Join(project, ".sdd", "meta.json")); !strings.Contains(meta, `"graph_schema_version": 1`) {
		t.Errorf("a write should leave the graph's schema alone:\n%s", meta)
	}

	output, err = runReleased(t, project, "new", "s", "tac", "--confidence", "high", "--skip-preflight", "--summary", "S.", "--title", "Explicit title", "Another gap.")
	if err != nil {
		t.Fatalf("an explicit title on a schema-1 graph should be accepted: %v\n%s", err, output)
	}
	written = gitIn(t, project, "show", "--name-only", "--format=", "HEAD")
	if entry := readFile(t, filepath.Join(project, strings.TrimSpace(written))); !strings.Contains(entry, "title: Explicit title") {
		t.Errorf("the explicit title should be written as given:\n%s", entry)
	}
}
