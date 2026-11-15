package cliapp_test

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/test"
)

// A running serve calls the model an llm change names on the next call, so
// the client and the window it is sized by never disagree.
func TestServeFollowsAnLLMChange(t *testing.T) {
	f := sessionFixture(t, false)
	answer := test.OpenAIBehaviour{Answer: `{"title": "Session capture", "summary": "A gap written into a session's capture.", "findings": []}`}
	first := test.NewOpenAIServer(t, answer)
	second := test.NewOpenAIServer(t, answer)
	llmLayer := func(server *test.OpenAIServer, model string) string {
		return "participant: Local Author\nllm:\n  provider: openai\n  model: " + model + "\n  endpoint: " + server.URL + "\n  api_keys:\n    openai: op-notarealkeyaaaaaaaaaa\n  retry:\n    max_attempts: 1\n"
	}
	// The sync timer titles entries in the background, so only the calls
	// carrying a capture's body tell which client the capture used.
	callsAbout := func(server *test.OpenAIServer, body string) []map[string]any {
		var calls []map[string]any
		for i, request := range server.Requests() {
			if strings.Contains(server.UserMessage(i), body) {
				calls = append(calls, request)
			}
		}
		return calls
	}
	firstBody := "The release checklist lacks a step that verifies the changelog names every breaking change."
	secondBody := "The onboarding guide lacks a section on how a newcomer runs the integration tests locally."
	f.write(t, ".sdd/config.local.yaml", llmLayer(first, "first-model"))

	f.serve(t, func(client *mcp.ClientSession) {
		opened, served, failed := callTool(t, client, "start_session", map[string]any{})
		if failed {
			t.Fatalf("start_session: %s", served)
		}
		capture := func(body string) {
			t.Helper()
			started, served, failed := callTool(t, client, "start_procedure", map[string]any{"session": opened.Session, "canonical": "capture"})
			if failed {
				t.Fatalf("start_procedure: %s", served)
			}
			report := captureReport()
			report["body"] = body
			playback, served, failed := callTool(t, client, "next", map[string]any{"session": opened.Session, "instance": started.Instance, "report": report})
			if failed || playback.Step != "playback" {
				t.Fatalf("the capture should reach playback, got step %q:\n%s", playback.Step, served)
			}
			_, served, failed = callTool(t, client, "next", map[string]any{"session": opened.Session, "instance": started.Instance,
				"report": map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "yes, capture it"}})
			if failed {
				t.Fatalf("the confirmed capture should write:\n%s", served)
			}
		}

		capture(firstBody)
		if len(callsAbout(first, firstBody)) == 0 {
			t.Fatal("the capture should call the configured model")
		}

		f.write(t, ".sdd/config.local.yaml", llmLayer(second, "second-model"))
		capture(secondBody)
		if len(callsAbout(first, secondBody)) != 0 {
			t.Error("after an llm change no call should reach the model configured before")
		}
		calls := callsAbout(second, secondBody)
		if len(calls) == 0 {
			t.Fatal("after an llm change the next capture should call the newly configured model")
		}
		for _, call := range calls {
			if call["model"] != "second-model" {
				t.Errorf("every call should name the newly configured model, got %v", call["model"])
			}
		}
	})
}
