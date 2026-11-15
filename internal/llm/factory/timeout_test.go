package factory_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

// outlastingClaude puts a `claude` executable on PATH whose answer outlasts
// every timeout these tests set.
func outlastingClaude(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in binary is a shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestTimeoutNamesTheSetting(t *testing.T) {
	outlastingClaude(t)
	runner, err := factory.New(model.LLMConfig{Provider: "claude-cli", Timeout: "200ms"})
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
	var attributed *llm.Error
	if !errors.As(err, &attributed) {
		t.Errorf("the timeout should keep the provider's attribution, got %v", err)
	}
}

func TestTimeoutNamesTheFlagThatSetIt(t *testing.T) {
	outlastingClaude(t)
	runner, err := factory.New(model.LLMConfig{Provider: "claude-cli", Timeout: "1m"}, factory.WithTimeoutFlag("preflight-timeout", 200*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(context.Background(), llm.Request{UserPrompt: "review"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a call outlasting the flag's bound should fail as a deadline, got %v", err)
	}
	if !strings.Contains(err.Error(), "--preflight-timeout") || strings.Contains(err.Error(), "llm.timeout") {
		t.Errorf("the timeout should name the flag that set the bound, not the setting it overrode, got %v", err)
	}
}

func TestCallerDeadlineIsNotBlamedOnTheSetting(t *testing.T) {
	outlastingClaude(t)
	runner, err := factory.New(model.LLMConfig{Provider: "claude-cli", Timeout: "1m"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = runner.Run(ctx, llm.Request{UserPrompt: "review"})
	if err == nil || strings.Contains(err.Error(), "llm.timeout") {
		t.Errorf("a deadline the caller set should not point at llm.timeout, got %v", err)
	}
}
