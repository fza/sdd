package cliapp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func hookBlock(event, payloadFile string) string {
	return "hooks:\n  - event: " + event + "\n    command: cat > " + payloadFile + "\n"
}

func readHookPayload(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the hook should have run and written its payload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("the hook should read a JSON payload on stdin, read %q: %v", raw, err)
	}
	return payload
}

func startSession(t *testing.T, client *mcp.ClientSession) string {
	t.Helper()
	door, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
	if err != nil || door.IsError {
		t.Fatalf("start_session: %v %+v", err, door)
	}
	handle, _ := door.StructuredContent.(map[string]any)["session"].(string)
	if handle == "" {
		t.Fatalf("start_session served no session handle: %+v", door.StructuredContent)
	}
	return handle
}

// A hook in the person's own config runs at its event with the payload on
// stdin (20261109-005138-d-tac-i61).
func TestServeRunsAHookFromThePersonsConfig(t *testing.T) {
	for _, file := range []string{".sdd/config.local.yaml", "config/sdd/config.yaml"} {
		t.Run(file, func(t *testing.T) {
			fixture := newAppFixture(t)
			payloadFile := filepath.Join(t.TempDir(), "payload.json")
			existing := readFile(t, filepath.Join(fixture.root, file))
			fixture.write(t, file, existing+hookBlock("user-dialogue.junction.entered", payloadFile))

			var handle string
			fixture.serve(t, func(client *mcp.ClientSession) { handle = startSession(t, client) })

			payload := readHookPayload(t, payloadFile)
			if payload["event"] != "user-dialogue.junction.entered" || payload["session"] != handle || payload["phase"] != "entered" {
				t.Errorf("the payload should name the event, the session and the phase, got %v", payload)
			}
		})
	}
}

// A hook added while serve runs reaches its next call.
func TestServeReadsHooksAfreshOnEveryCall(t *testing.T) {
	fixture := newAppFixture(t)
	payloadFile := filepath.Join(t.TempDir(), "payload.json")
	fixture.serve(t, func(client *mcp.ClientSession) {
		handle := startSession(t, client)
		existing := readFile(t, filepath.Join(fixture.root, ".sdd/config.local.yaml"))
		fixture.write(t, ".sdd/config.local.yaml", existing+hookBlock("capture.assemble.entered", payloadFile))
		result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_procedure", Arguments: map[string]any{"session": handle, "canonical": "capture"}})
		if err != nil || result.IsError {
			t.Fatalf("start_procedure: %v %+v", err, result)
		}
	})
	if payload := readHookPayload(t, payloadFile); payload["procedure"] != "capture" || payload["draft"] == nil {
		t.Errorf("a capture event should carry the procedure and the draft, got %v", payload)
	}
}
