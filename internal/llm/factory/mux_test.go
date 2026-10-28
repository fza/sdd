package factory_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

// A schema option is client-level in gollm, so whether each purpose carries
// its own schema is only observable on the wire. This drives every purpose
// through one composed runner against a local listener and asserts on the
// bytes each produced.
func TestPurposeMuxConstrainsEachPurposeToItsSchema(t *testing.T) {
	var mu sync.Mutex
	bodies := map[string]map[string]any{}
	var current string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			_, _ = w.Write([]byte(`{"models":[]}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		bodies[current] = body
		mu.Unlock()
		_, _ = w.Write([]byte(`{"model":"m","response":"{\"findings\":[]}","done":true}`))
	}))
	defer srv.Close()

	runner, err := factory.New(model.LLMConfig{
		Provider: "ollama", Model: "m", OllamaEndpoint: srv.URL,
	})
	if err != nil {
		t.Fatalf("factory.New: %v", err)
	}

	wantProperty := map[llm.Purpose]string{
		llm.PurposeSummarize:    "summary",
		llm.PurposeTitle:        "title",
		llm.PurposePreflight:    "findings",
		llm.PurposeWritingGuide: "findings",
	}
	for purpose := range wantProperty {
		mu.Lock()
		current = string(purpose)
		mu.Unlock()
		if _, err := runner.Run(context.Background(), llm.Request{Purpose: purpose, UserPrompt: "x"}); err != nil {
			t.Fatalf("Run(%s): %v", purpose, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()

	for purpose, property := range wantProperty {
		schema, ok := bodies[string(purpose)]["format"].(map[string]any)
		if !ok {
			t.Fatalf("%s: format key missing, got %v", purpose, bodies[string(purpose)])
		}
		if schema["additionalProperties"] != false {
			t.Errorf("%s: schema must close the object, got %v", purpose, schema["additionalProperties"])
		}
		if _, ok := schema["properties"].(map[string]any)[property]; !ok {
			t.Errorf("%s: schema lacks the purpose's answer property: %v", purpose, schema)
		}
	}
	if _, ok := bodies[string(llm.PurposeTitle)]["format"].(map[string]any)["properties"].(map[string]any)["summary"]; ok {
		t.Error("the title-only schema must not ask for a summary")
	}
}

// The unconstrained client is built eagerly so a misconfiguration fails the
// command, not the first call that happens to need a checker.
func TestPurposeMuxRejectsMissingAPIKeyBeforeAnyCall(t *testing.T) {
	if _, err := factory.New(model.LLMConfig{Provider: "openai", Model: "gpt-5"}); err == nil {
		t.Fatal("a missing API key must fail composition, not the first call")
	}
}
