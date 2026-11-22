package cliapp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeOpenAI is an OpenAI-compatible chat server (POST /v1/chat/completions)
// that answers every call with no findings and records the model and the
// messages each call carried.
type fakeOpenAI struct {
	mu    sync.Mutex
	calls []openAICall
}

type openAICall struct {
	model, messages string
}

func (f *fakeOpenAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/chat/completions" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var req struct {
		Model    string            `json:"model"`
		Messages []json.RawMessage `json:"messages"`
	}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var messages []string
	for _, message := range req.Messages {
		messages = append(messages, string(message))
	}
	f.mu.Lock()
	f.calls = append(f.calls, openAICall{model: req.Model, messages: strings.Join(messages, "\n")})
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"content": `{"findings": []}`}}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
	})
}

// callsAbout returns the calls whose messages carry text.
func (f *fakeOpenAI) callsAbout(text string) []openAICall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var calls []openAICall
	for _, call := range f.calls {
		if strings.Contains(call.messages, text) {
			calls = append(calls, call)
		}
	}
	return calls
}

func newFakeOpenAI(t *testing.T) (*fakeOpenAI, string) {
	t.Helper()
	fake := &fakeOpenAI{}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, server.URL
}

// A running serve calls the model an llm change names on the next call.
func TestServeFollowsAnLLMChange(t *testing.T) {
	fixture := newAppFixture(t)
	first, firstURL := newFakeOpenAI(t)
	second, secondURL := newFakeOpenAI(t)
	llmLayer := func(endpoint, model string) string {
		return "participant: Local Author\nllm:\n  provider: openai\n  model: " + model + "\n  endpoint: " + endpoint +
			"\n  api_keys:\n    openai: op-notarealkeyaaaaaaaaaa\n  retry:\n    max_attempts: 1\n"
	}
	firstBody := "The release checklist lacks a step that verifies the changelog names every breaking change."
	secondBody := "The onboarding guide lacks a section on how a newcomer runs the integration tests locally."
	fixture.write(t, ".sdd/config.local.yaml", llmLayer(firstURL, "first-model"))

	fixture.serve(t, func(client *mcp.ClientSession) {
		session := openServedSession(t, client)
		draft := func(body string) {
			t.Helper()
			started, served, failed := callStepTool(t, client, "start_procedure", map[string]any{"session": session, "canonical": "capture"})
			if failed {
				t.Fatalf("start_procedure: %s", served)
			}
			report := map[string]any{
				"body": body, "entryKind": "gap", "layer": "tactical", "topics": []string{"testing/llm"},
				"confidence": "low", "widenReport": "searched the graph for onboarding and release records; it holds none.",
			}
			playback, served, failed := callStepTool(t, client, "next", map[string]any{"session": session, "instance": started.Instance, "report": report})
			if failed || playback.Step != "playback" {
				t.Fatalf("the draft should reach playback, got step %q:\n%s", playback.Step, served)
			}
		}

		draft(firstBody)
		if len(first.callsAbout(firstBody)) == 0 {
			t.Fatal("the draft should call the configured model")
		}

		fixture.write(t, ".sdd/config.local.yaml", llmLayer(secondURL, "second-model"))
		draft(secondBody)
		if len(first.callsAbout(secondBody)) != 0 {
			t.Error("after an llm change no call should reach the model configured before")
		}
		calls := second.callsAbout(secondBody)
		if len(calls) == 0 {
			t.Fatal("after an llm change the next draft should call the newly configured model")
		}
		for _, call := range calls {
			if call.model != "second-model" {
				t.Errorf("every call should name the newly configured model, got %q", call.model)
			}
		}
	})
}

type servedStep struct {
	Instance string `json:"instance"`
	Step     string `json:"step"`
}

// callStepTool calls a tool and returns its serve, the text it answered with,
// and whether the call failed.
func callStepTool(t *testing.T, client *mcp.ClientSession, tool string, args map[string]any) (servedStep, string, bool) {
	t.Helper()
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var serve servedStep
	_ = json.Unmarshal(encoded, &serve)
	var text strings.Builder
	for _, content := range result.Content {
		if part, ok := content.(*mcp.TextContent); ok {
			text.WriteString(part.Text)
		}
	}
	return serve, text.String() + string(encoded), result.IsError
}
