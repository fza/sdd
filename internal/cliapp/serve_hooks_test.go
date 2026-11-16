package cliapp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// payloadHook declares, in the config file named, a hook at event whose
// script writes its payload beside its hooks folder, and returns the
// payload's path.
func (f *appFixture) payloadHook(t *testing.T, configFile, event string) string {
	t.Helper()
	hooksDir := filepath.Join(filepath.Dir(configFile), "hooks")
	f.write(t, filepath.Join(hooksDir, "payload.sh"), "#!/bin/sh\ncat > ../payload.json\n")
	err := os.Chmod(filepath.Join(f.root, hooksDir, "payload.sh"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	existing := readFile(t, filepath.Join(f.root, configFile))
	f.write(t, configFile, existing+"hooks:\n  - event: "+event+"\n    command: payload.sh\n")
	return filepath.Join(f.root, filepath.Dir(configFile), "payload.json")
}

func readHookPayload(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the hook should have run and written its payload: %v", err)
	}
	var payload map[string]any
	err = json.Unmarshal(raw, &payload)
	if err != nil {
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

// Hooks of every layer run at their event, the payload on stdin; the
// committed layer's once allowed.
func TestServeRunsTheHooksOfEveryLayer(t *testing.T) {
	for _, configFile := range []string{"config/sdd/config.yaml", ".sdd/config.yaml", ".sdd/config.local.yaml"} {
		t.Run(configFile, func(t *testing.T) {
			fixture := newAppFixture(t)
			payloadFile := fixture.payloadHook(t, configFile, "user-dialogue.junction.start")
			if configFile == ".sdd/config.yaml" {
				allowed := fixture.run(t, "hooks", "allow")
				if allowed.err != nil {
					t.Fatalf("sdd hooks allow: %v\n%s", allowed.err, allowed.stderr)
				}
			}

			var handle string
			fixture.serve(t, func(client *mcp.ClientSession) { handle = startSession(t, client) })

			payload := readHookPayload(t, payloadFile)
			if payload["event"] != "user-dialogue.junction.start" || payload["session"] != handle || payload["phase"] != "start" {
				t.Errorf("the payload should name the event, the session and the phase, got %v", payload)
			}
		})
	}
}

// A hook that came with the repository runs only once `sdd hooks allow`
// recorded it on this machine; until then the call goes on without it and
// says so.
func TestServeSkipsACommittedHookUntilAllowed(t *testing.T) {
	fixture := newAppFixture(t)
	payloadFile := fixture.payloadHook(t, ".sdd/config.yaml", "user-dialogue.junction.start")

	var text string
	fixture.serve(t, func(client *mcp.ClientSession) {
		door, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
		if err != nil || door.IsError {
			t.Fatalf("start_session: %v %+v", err, door)
		}
		raw, _ := json.Marshal(door)
		text = string(raw)
	})
	_, err := os.Stat(payloadFile)
	if err == nil {
		t.Fatal("a committed hook not allowed on this machine ran")
	}
	if !strings.Contains(text, "sdd hooks allow") {
		t.Errorf("the call should name `sdd hooks allow` for the skipped hook, served %s", text)
	}

	allowed := fixture.run(t, "hooks", "allow")
	if allowed.err != nil || !strings.Contains(allowed.stdout, "payload.sh") {
		t.Fatalf("sdd hooks allow should name the hook it allows:\n%s%v", allowed.stdout, allowed.err)
	}
	fixture.serve(t, func(client *mcp.ClientSession) { startSession(t, client) })
	readHookPayload(t, payloadFile)
}

// A hook added while serve runs reaches its next call.
func TestServeReadsHooksAfreshOnEveryCall(t *testing.T) {
	fixture := newAppFixture(t)
	var payloadFile string
	fixture.serve(t, func(client *mcp.ClientSession) {
		handle := startSession(t, client)
		payloadFile = fixture.payloadHook(t, "config/sdd/config.yaml", "capture.assemble.start")
		result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_procedure", Arguments: map[string]any{"session": handle, "canonical": "capture"}})
		if err != nil || result.IsError {
			t.Fatalf("start_procedure: %v %+v", err, result)
		}
	})
	payload := readHookPayload(t, payloadFile)
	if payload["procedure"] != "capture" || payload["draft"] == nil {
		t.Errorf("a capture event should carry the procedure and the draft, got %v", payload)
	}
}
