package factory_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

// The default provider runs at its default effort unless llm.effort sets one.
func TestClaudeCLIRunsAtTheConfiguredEffort(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do printf '%s\\0' \"$a\"; done > '" + dir + "/args'\n" +
		"cat > /dev/null\n" +
		"echo '{\"type\":\"result\",\"result\":\"OK\",\"usage\":{}}'\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, tc := range []struct {
		effort, want string
	}{
		{"", "medium"},
		{"high", "high"},
	} {
		runner, err := factory.New(model.LLMConfig{Effort: tc.effort})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if _, err := runner.Run(context.Background(), llm.Request{UserPrompt: "U"}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "args"))
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
		if i := slices.Index(args, "--effort"); i < 0 || i+1 >= len(args) || args[i+1] != tc.want {
			t.Errorf("llm.effort %q: args %q, want --effort %s", tc.effort, args, tc.want)
		}
	}
}
