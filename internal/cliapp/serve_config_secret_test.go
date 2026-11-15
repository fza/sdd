package cliapp_test

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A value in the wrong place, which may be a secret, never reaches the agent
// in the refusal.
func TestServeRefusalNeverShowsAConfigValue(t *testing.T) {
	fixture := newAppFixture(t)
	fixture.serve(t, func(client *mcp.ClientSession) {
		session, _ := openSession(t, client)

		fixture.write(t, ".sdd/config.local.yaml", "participant: Local Author\nllm:\n  api_keys: sk-proj-notarealsecret\n")
		_, failure := callToolResult(t, client, "resume_session", map[string]any{"session": session})
		if failure == "" {
			t.Fatal("a scalar where a map belongs should refuse the call")
		}
		if strings.Contains(failure, "sk-") {
			t.Errorf("the refusal should not show the value: %s", failure)
		}
		if !strings.Contains(failure, "llm.api_keys") || !strings.Contains(failure, "line 3") {
			t.Errorf("the refusal should name the key and the line: %s", failure)
		}
	})
}
