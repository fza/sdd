package factory_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

// slowClaude puts a `claude` executable on PATH whose answer outlasts any
// bound a test sets.
func slowClaude(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestExtractionTimeoutNamesItsOwnSetting(t *testing.T) {
	slowClaude(t)
	runner, err := factory.New(model.LLMConfig{Provider: "claude-cli", Timeout: "1m", ExtractTimeout: "200ms"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(context.Background(), llm.Request{UserPrompt: "reformat", Purpose: llm.PurposeWritingGuideExtract})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("an extraction call outlasting its bound should fail as a deadline, got %v", err)
	}
	if !strings.Contains(err.Error(), "sdd config set llm.extract_timeout") {
		t.Errorf("an extraction timeout should name the setting that bounds it, got %v", err)
	}
}

func TestCallerDeadlineOnExtractionIsNotBlamedOnTheSetting(t *testing.T) {
	slowClaude(t)
	runner, err := factory.New(model.LLMConfig{Provider: "claude-cli", ExtractTimeout: "1m"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = runner.Run(ctx, llm.Request{UserPrompt: "reformat", Purpose: llm.PurposePreflightExtract})
	if err == nil || strings.Contains(err.Error(), "llm.extract_timeout") {
		t.Errorf("a deadline the caller set should not point at llm.extract_timeout, got %v", err)
	}
}
