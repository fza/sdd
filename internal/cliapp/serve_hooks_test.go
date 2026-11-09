package cliapp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
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
	if err := os.Chmod(filepath.Join(f.root, hooksDir, "payload.sh"), 0o755); err != nil {
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

func toolText(result *mcp.CallToolResult) string {
	var text strings.Builder
	for _, content := range result.Content {
		if c, ok := content.(*mcp.TextContent); ok {
			text.WriteString(c.Text)
		}
	}
	return text.String()
}

// A hook in the user-global config runs at its event without confirmation,
// its payload on stdin (20261109-213250-d-tac-bve).
func TestServeRunsAUserGlobalHookUnconfirmed(t *testing.T) {
	fixture := newAppFixture(t)
	payloadFile := fixture.payloadHook(t, "config/sdd/config.yaml", "user-dialogue.junction.start")

	var handle string
	fixture.serve(t, func(client *mcp.ClientSession) { handle = startSession(t, client) })

	payload := readHookPayload(t, payloadFile)
	if payload["event"] != "user-dialogue.junction.start" || payload["session"] != handle || payload["phase"] != "start" {
		t.Errorf("the payload should name the event, the session and the phase, got %v", payload)
	}
}

// A project's hook runs only once confirmed: over MCP the call asks the
// person by elicitation and continues on accept; a decline or a client
// without elicitation fails the call naming `sdd hooks trust`.
func TestServeAsksForAnUnconfirmedProjectHook(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answer  string
		elicits bool
	}{
		{name: "accepted", answer: "accept", elicits: true},
		{name: "declined", answer: "decline", elicits: true},
		{name: "cancelled", answer: "cancel", elicits: true},
		{name: "client without elicitation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAppFixture(t)
			payloadFile := fixture.payloadHook(t, ".sdd/config.yaml", "user-dialogue.junction.start")
			var (
				mu       sync.Mutex
				messages []string
			)
			var options *mcp.ClientOptions
			if tc.elicits {
				options = &mcp.ClientOptions{ElicitationHandler: func(_ context.Context, request *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
					mu.Lock()
					messages = append(messages, request.Params.Message)
					mu.Unlock()
					return &mcp.ElicitResult{Action: tc.answer}, nil
				}}
			}

			var result *mcp.CallToolResult
			fixture.serveWith(t, options, func(client *mcp.ClientSession) {
				var err error
				result, err = client.CallTool(t.Context(), &mcp.CallToolParams{Name: "start_session", Arguments: map[string]any{}})
				if err != nil {
					t.Fatal(err)
				}
			})

			_, statErr := os.Stat(payloadFile)
			if tc.answer != "accept" {
				if !result.IsError || !strings.Contains(toolText(result), "sdd hooks trust") || !strings.Contains(toolText(result), filepath.Join(".sdd", "config.yaml")) {
					t.Fatalf("the call should fail naming the hook's file and `sdd hooks trust`, got %+v", result)
				}
				if statErr == nil {
					t.Fatal("an unconfirmed hook ran")
				}
				return
			}
			if result.IsError {
				t.Fatalf("an accepted hook should let the call continue, got %s", toolText(result))
			}
			if len(messages) != 1 || !strings.Contains(messages[0], "payload.sh") || !strings.Contains(messages[0], "cat > ../payload.json") {
				t.Errorf("the person should see the hook and its script once, got %q", messages)
			}
			readHookPayload(t, payloadFile)
			if list := fixture.run(t, "hooks"); !regexp.MustCompile(`confirmed +committed +user-dialogue\.junction\.start +payload\.sh`).MatchString(list.stdout) {
				t.Errorf("the accepted hook should list as confirmed:\n%s%v", list.stdout, list.err)
			}
		})
	}
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
	if payload := readHookPayload(t, payloadFile); payload["procedure"] != "capture" || payload["draft"] == nil {
		t.Errorf("a capture event should carry the procedure and the draft, got %v", payload)
	}
}

// `sdd hooks` lists every hook with its state; `sdd hooks trust` shows each
// pending one and confirms it on yes; a changed script asks again.
func TestHooksTrustConfirmsWhatThePersonAccepts(t *testing.T) {
	fixture := newAppFixture(t)
	fixture.payloadHook(t, ".sdd/config.yaml", "capture.assemble")
	fixture.payloadHook(t, "config/sdd/config.yaml", "entry.written")
	row := func(state, layer, event string) *regexp.Regexp {
		return regexp.MustCompile(state + ` +` + layer + ` +` + regexp.QuoteMeta(event) + ` +payload\.sh`)
	}

	list := fixture.run(t, "hooks")
	if list.err != nil || !row("pending", "committed", "capture.assemble").MatchString(list.stdout) || !row("confirmed", "global", "entry.written").MatchString(list.stdout) {
		t.Fatalf("sdd hooks should list the project's hook pending and the global one confirmed:\n%s%v", list.stdout, list.err)
	}

	declined := fixture.runInput(t, "n\n", "hooks", "trust")
	if declined.err != nil || !strings.Contains(declined.stdout, "cat > ../payload.json") || !strings.Contains(declined.stdout, "[y/N]") {
		t.Fatalf("sdd hooks trust should show the script and ask:\n%s%v", declined.stdout, declined.err)
	}
	if list := fixture.run(t, "hooks"); !row("pending", "committed", "capture.assemble").MatchString(list.stdout) {
		t.Fatalf("a declined hook should stay pending:\n%s", list.stdout)
	}

	accepted := fixture.runInput(t, "y\n", "hooks", "trust")
	if accepted.err != nil || !strings.Contains(accepted.stdout, "confirmed") {
		t.Fatalf("sdd hooks trust should confirm on yes:\n%s%v", accepted.stdout, accepted.err)
	}
	if list := fixture.run(t, "hooks"); !row("confirmed", "committed", "capture.assemble").MatchString(list.stdout) {
		t.Fatalf("an accepted hook should list as confirmed:\n%s", list.stdout)
	}

	fixture.write(t, ".sdd/hooks/payload.sh", "#!/bin/sh\ncat > ../elsewhere.json\n")
	if list := fixture.run(t, "hooks"); !row("changed", "committed", "capture.assemble").MatchString(list.stdout) {
		t.Fatalf("a changed script should list as changed:\n%s", list.stdout)
	}
}

// sdd config lists the hooks of every layer, the committed one included.
func TestConfigListsTheCommittedLayersHooks(t *testing.T) {
	fixture := newAppFixture(t)
	fixture.payloadHook(t, ".sdd/config.yaml", "capture.assemble")
	result := fixture.run(t, "config")
	if result.err != nil || !regexp.MustCompile(`hooks\.0\.command +payload\.sh +\(project\)`).MatchString(result.stdout) {
		t.Fatalf("sdd config should list the committed hook:\n%s%v", result.stdout, result.err)
	}
}

// In a project_dir layout both repositories declare hooks, in both of their
// config files; sdd hooks and sdd config list every layer in order.
func TestHooksOfBothRepositoriesOfAProjectDirLayout(t *testing.T) {
	work, project := pointedRepos(t)
	declare := func(repo, file, command string) {
		path := filepath.Join(repo, ".sdd", file)
		existing := ""
		if raw, err := os.ReadFile(path); err == nil {
			existing = string(raw)
		}
		writeFile(t, path, existing+"hooks:\n  - event: capture.assemble\n    command: "+command+"\n")
		writeFile(t, filepath.Join(repo, ".sdd", "hooks", command), "#!/bin/sh\n")
		if err := os.Chmod(filepath.Join(repo, ".sdd", "hooks", command), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	declare(work, "config.yaml", "work.sh")
	declare(work, "config.local.yaml", "local/work-local.sh")
	declare(project, "config.yaml", "project.sh")
	declare(project, "config.local.yaml", "local/project-local.sh")

	listed, err := runAppErr(t, work, "hooks")
	if err != nil {
		t.Fatalf("sdd hooks: %v\n%s", err, listed)
	}
	order := regexp.MustCompile(`(?s)pending +work committed .*work\.sh.*pending +work local .*local/work-local\.sh.*pending +project committed .*project\.sh.*pending +project local .*local/project-local\.sh`)
	if !order.MatchString(listed) {
		t.Errorf("sdd hooks should list every repository's layers, work before project, committed before local:\n%s", listed)
	}

	config, err := runAppErr(t, work, "config")
	if err != nil {
		t.Fatalf("sdd config: %v\n%s", err, config)
	}
	for _, row := range []string{`hooks\.0\.command +work\.sh +\(work\)`, `hooks\.1\.command +local/work-local\.sh +\(work local\)`, `hooks\.2\.command +project\.sh +\(project\)`, `hooks\.3\.command +local/project-local\.sh +\(local\)`} {
		if !regexp.MustCompile(row).MatchString(config) {
			t.Errorf("sdd config should list %s:\n%s", row, config)
		}
	}
}

// sdd init names the pending hooks when it cannot ask on a terminal.
func TestInitNamesThePendingHooks(t *testing.T) {
	work, _ := pointedRepos(t)
	writeFile(t, filepath.Join(work, ".sdd", "config.yaml"), "project_dir: ../work-sdd\nskill_scope: project\nsupported_agents: [claude]\nhooks:\n  - event: capture.assemble\n    command: lint.sh\n")
	writeFile(t, filepath.Join(work, ".sdd", "hooks", "lint.sh"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(work, ".sdd", "hooks", "lint.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := runAppErr(t, work, "init", "--scope", "project", "--participant", "Ada", "--agents", "claude")
	if err != nil {
		t.Fatalf("sdd init: %v\n%s", err, output)
	}
	if !strings.Contains(output, "1 hook(s) of this project wait for confirmation; review them with `sdd hooks trust`") {
		t.Errorf("sdd init should name the pending hooks:\n%s", output)
	}
}
