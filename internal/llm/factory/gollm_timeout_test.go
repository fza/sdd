package factory_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

func TestGollmTimeoutNamesTheSetting(t *testing.T) {
	done := make(chan struct{})
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		select {
		case <-r.Context().Done():
		case <-done:
		}
	}))
	t.Cleanup(stalled.Close)
	t.Cleanup(func() { close(done) })
	once := model.RetryConfig{MaxAttempts: 1}
	for _, tc := range []struct {
		provider string
		cfg      model.LLMConfig
	}{
		{"anthropic", model.LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4-6", Endpoint: stalled.URL, APIKeys: map[string]string{"anthropic": "k"}, Retry: once}},
		{"openai", model.LLMConfig{Provider: "openai", Model: "gpt-x", Endpoint: stalled.URL, APIKeys: map[string]string{"openai": "k"}, Retry: once}},
		{"ollama", model.LLMConfig{Provider: "ollama", Model: "qwen3:8b", OllamaEndpoint: stalled.URL, Retry: once}},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			tc.cfg.Timeout = "200ms"
			runner, err := factory.New(tc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			_, err = runner.Run(context.Background(), llm.Request{UserPrompt: "review"})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("a call outlasting its bound should fail as a deadline, got %v", err)
			}
			if !strings.Contains(err.Error(), "sdd config set llm.timeout") {
				t.Errorf("the timeout should name the setting that raises it, got %v", err)
			}
		})
	}
}

// A deadline the caller set itself is the caller's, so the error names no
// setting.
func TestCallerDeadlinePassesThroughUnnamed(t *testing.T) {
	done := make(chan struct{})
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-done:
		}
	}))
	t.Cleanup(stalled.Close)
	t.Cleanup(func() { close(done) })
	runner, err := factory.New(model.LLMConfig{Provider: "openai", Model: "gpt-x", Endpoint: stalled.URL, APIKeys: map[string]string{"openai": "k"}, Retry: model.RetryConfig{MaxAttempts: 1}, Timeout: "1m"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = runner.Run(ctx, llm.Request{UserPrompt: "review"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the caller's deadline should end the call, got %v", err)
	}
	if strings.Contains(err.Error(), "sdd config set") {
		t.Errorf("a caller's own deadline should name no setting, got %v", err)
	}
}
