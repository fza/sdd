package cliapp_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The server opens sessions in the dialogue mode the person's config sets;
// the committed config cannot set it (20261029-150854-d-tac-b2f).
func TestServeOpensSessionsInTheConfiguredDialogueMode(t *testing.T) {
	cases := []struct {
		name           string
		file           string
		wantAutonomous bool
	}{
		{name: "local config", file: ".sdd/config.local.yaml", wantAutonomous: true},
		{name: "global config", file: "config/sdd/config.yaml", wantAutonomous: true},
		{name: "committed config", file: ".sdd/config.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAppFixture(t)
			existing := readFile(t, filepath.Join(fixture.root, tc.file))
			fixture.write(t, tc.file, existing+"dialogue:\n  mode: autonomous\n")

			var opening string
			fixture.serve(t, func(client *mcp.ClientSession) {
				door, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
				if err != nil || door.IsError {
					t.Fatalf("start_session: %v %+v", err, door)
				}
				encoded, err := json.Marshal(door.StructuredContent)
				if err != nil {
					t.Fatal(err)
				}
				opening = string(encoded)
			})
			if got := strings.Contains(opening, "working on your own"); got != tc.wantAutonomous {
				t.Errorf("only a person's own config should open the session autonomous, got autonomous=%v", got)
			}
		})
	}
}
