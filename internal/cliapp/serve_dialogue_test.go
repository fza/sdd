package cliapp_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/test"
)

// The server opens sessions in the dialogue mode the person's config sets;
// the committed config cannot set it.
func TestServeOpensSessionsInTheConfiguredDialogueMode(t *testing.T) {
	cases := []struct {
		name           string
		file           string
		mode           string
		wantAutonomous bool
	}{
		{name: "local config", file: ".sdd/config.local.yaml", mode: "auto", wantAutonomous: true},
		{name: "global config", file: "config/sdd/config.yaml", mode: "auto", wantAutonomous: true},
		{name: "committed config", file: ".sdd/config.yaml", mode: "auto"},
		{name: "unknown value in the committed config", file: ".sdd/config.yaml", mode: "autonomous"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAppFixture(t)
			existing := readFile(t, filepath.Join(fixture.root, tc.file))
			fixture.write(t, tc.file, existing+"dialogue:\n  mode: "+tc.mode+"\n")

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
				opening = withWaitingLanes(t, client, test.Untagged(string(encoded)))
			})
			if got := strings.Contains(opening, "working on your own"); got != tc.wantAutonomous {
				t.Errorf("only auto in a person's own config should open the session autonomous, got autonomous=%v", got)
			}
		})
	}
}

// A dialogue mode sdd does not know in a person's config stops the server
// before any session opens guided in its place.
func TestServeRefusesAnUnknownDialogueMode(t *testing.T) {
	cases := []struct {
		name string
		file string
		args []string
	}{
		{name: "local config", file: ".sdd/config.local.yaml"},
		{name: "global config", file: "config/sdd/config.yaml"},
		{name: "local config flag", file: "override.yaml", args: []string{"--local-config", "override.yaml"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAppFixture(t)
			fixture.write(t, tc.file, "participant: Ada\ndialogue:\n  mode: autonomous\n")

			result := fixture.run(t, append(tc.args, "serve", "--transport", "stdio")...)
			if result.err == nil {
				t.Fatal("serve should refuse a config carrying an unknown dialogue mode")
			}
			for _, part := range []string{tc.file, "autonomous", "guided or auto"} {
				if !strings.Contains(result.err.Error(), part) {
					t.Errorf("the refusal should name the file, the value and the valid values, missing %q: %v", part, result.err)
				}
			}
		})
	}
}
