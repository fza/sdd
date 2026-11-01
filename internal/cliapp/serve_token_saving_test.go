package cliapp_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Only the user-global config turns the served-once memory off
// (20261101-164201-d-tac-sj4).
func TestServeHonorsTokenSavingFromTheGlobalConfigOnly(t *testing.T) {
	cases := []struct {
		name        string
		file        string
		wantStubbed bool
	}{
		{name: "unset", wantStubbed: true},
		{name: "global config", file: "config/sdd/config.yaml"},
		{name: "local config", file: ".sdd/config.local.yaml", wantStubbed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAppFixture(t)
			if tc.file != "" {
				existing := readFile(t, filepath.Join(fixture.root, tc.file))
				fixture.write(t, tc.file, existing+"sessions:\n  token_saving: off\n")
			}

			var second string
			fixture.serve(t, func(client *mcp.ClientSession) {
				door := callStructured(t, client, "start_session", map[string]any{})
				session, _ := door["session"].(string)
				callStructured(t, client, "start_procedure", map[string]any{"session": session, "canonical": "capture"})
				encoded, err := json.Marshal(callStructured(t, client, "start_procedure", map[string]any{"session": session, "canonical": "capture"})["report_schema"])
				if err != nil {
					t.Fatal(err)
				}
				second = string(encoded)
			})
			if got := strings.Contains(second, "served_earlier"); got != tc.wantStubbed {
				t.Errorf("a repeated report schema should be stubbed unless the global config turns token saving off, got stubbed=%v", got)
			}
		})
	}
}

func callStructured(t *testing.T, client *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil || result.IsError {
		t.Fatalf("%s: %v %+v", name, err, result)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
