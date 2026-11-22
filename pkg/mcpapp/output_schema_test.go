package mcpapp_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolsDeclareTheirOutputSchemaOnlyWhenAsked(t *testing.T) {
	cases := []struct {
		name         string
		outputSchema bool
	}{
		{name: "default"},
		{name: "asked", outputSchema: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestServer(t, nil, "", "", func(o *testServerOptions) { o.MCP.OutputSchema = tc.outputSchema })
			cs := connect(t, env.srv)
			res, err := cs.ListTools(t.Context(), &mcp.ListToolsParams{})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Tools) == 0 {
				t.Fatal("tools/list returned no tools")
			}
			for _, tool := range res.Tools {
				if !tc.outputSchema {
					if tool.OutputSchema != nil {
						t.Errorf("tool %s declares an outputSchema nobody asked for", tool.Name)
					}
					continue
				}
				encoded, err := json.Marshal(tool.OutputSchema)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(encoded), `"properties"`) {
					t.Errorf("tool %s should declare the outputSchema derived from its result type, got %s", tool.Name, encoded)
				}
			}
		})
	}
}

// The SDK validates each result against the declared schema, so a declared
// schema must admit what the tools return.
func TestDeclaredOutputSchemaAdmitsTheResult(t *testing.T) {
	env := newTestServer(t, nil, "", "", func(o *testServerOptions) { o.MCP.OutputSchema = true })
	cs := connect(t, env.srv)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("start_session failed under a declared outputSchema: %+v", res.Content)
	}
}
