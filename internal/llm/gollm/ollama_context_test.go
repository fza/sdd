package gollm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"

	gollmrunner "github.com/networkteam/sdd/internal/llm/gollm"
)

// Ollama cuts a prompt longer than num_ctx without an error, so every call
// carries a num_ctx covering its prompt and the answer reserve.
func TestOllamaCallsCarryAContextCoveringThePrompt(t *testing.T) {
	var mu sync.Mutex
	var sizes []float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_, _ = w.Write([]byte(`{"models":[]}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Options map[string]any `json:"options"`
		}
		_ = json.Unmarshal(raw, &body)
		size, _ := body.Options["num_ctx"].(float64)
		mu.Lock()
		sizes = append(sizes, size)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"model":"m","response":"ok","done":true}`))
	}))
	defer srv.Close()

	const reserve = 8192
	runner, err := gollmrunner.NewRunner(model.LLMConfig{Provider: "ollama", Model: "m", OllamaEndpoint: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	short := llm.Request{SystemPrompt: "S", UserPrompt: "U"}
	long := llm.Request{SystemPrompt: "S", UserPrompt: strings.Repeat("word ", 30000)}
	shortish := llm.Request{SystemPrompt: "S", UserPrompt: strings.Repeat("word ", 200)}
	for _, req := range []llm.Request{short, long, shortish} {
		_, err := runner.Run(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sizes) != 3 {
		t.Fatalf("every call should reach the server, got %v", sizes)
	}
	if sizes[2] != sizes[0] {
		t.Errorf("calls of similar size should share one num_ctx, so Ollama keeps the model loaded, got %v", sizes)
	}
	if sizes[0] < float64(reserve) {
		t.Errorf("num_ctx should leave room for the answer reserve, got %v", sizes[0])
	}
	if sizes[1]*3 < float64(len(long.UserPrompt))+float64(reserve)*3 {
		t.Errorf("num_ctx should cover the long prompt and the reserve, got %v", sizes[1])
	}
}

// Calls of one runner reach Ollama side by side: a prefilter pass under one
// deadline scores its pairs concurrently.
func TestOllamaCallsRunConcurrently(t *testing.T) {
	var mu sync.Mutex
	inFlight, most := 0, 0
	both := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_, _ = w.Write([]byte(`{"models":[]}`))
			return
		}
		mu.Lock()
		inFlight++
		most = max(most, inFlight)
		if inFlight == 2 {
			close(both)
		}
		mu.Unlock()
		select {
		case <-both:
		case <-time.After(2 * time.Second):
		}
		mu.Lock()
		inFlight--
		mu.Unlock()
		_, _ = w.Write([]byte(`{"model":"m","response":"ok","done":true}`))
	}))
	defer srv.Close()

	runner, err := gollmrunner.NewRunner(model.LLMConfig{Provider: "ollama", Model: "m", OllamaEndpoint: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if _, err := runner.Run(context.Background(), llm.Request{SystemPrompt: "S", UserPrompt: "U"}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if most != 2 {
		t.Fatalf("two calls should reach Ollama at once, at most %d did", most)
	}
}
