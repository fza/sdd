package codex_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llm/codex"
	"github.com/networkteam/sdd/pkg/llm"
)

const wireSchema = `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`

// fakeCLI puts a codex executable on PATH that records its arguments, stdin,
// working directory and the schema file it was handed into dir, and prints
// events as codex exec's JSONL output.
func fakeCLI(t *testing.T, events string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in binary is a shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do printf '%s\\0' \"$a\"; done > '" + dir + "/args'\n" +
		"pwd > '" + dir + "/cwd'\n" +
		"env > '" + dir + "/env'\n" +
		"prev=''\nfor a in \"$@\"; do if [ \"$prev\" = --output-schema ]; then cp \"$a\" '" + dir + "/schema'; fi; prev=\"$a\"; done\n" +
		"cat > '" + dir + "/stdin'\n" +
		"cat '" + dir + "/events.jsonl'\n"
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func recorded(t *testing.T, dir, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func recordedArgs(t *testing.T, dir string) []string {
	t.Helper()
	return strings.Split(strings.TrimSuffix(recorded(t, dir, "args"), "\x00"), "\x00")
}

const answered = `{"type":"thread.started","thread_id":"t-1"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"{\"answer\":\"ok\"}"}}
{"type":"turn.completed","usage":{"input_tokens":120,"cached_input_tokens":20,"output_tokens":15,"reasoning_output_tokens":4}}
`

// A structured request hands the schema to --output-schema byte for byte,
// runs isolated with the model and effort as written, sends the prompt on
// stdin and returns the final message with the turn's usage.
func TestRunWithResponseSchema(t *testing.T) {
	dir := fakeCLI(t, answered)
	req := llm.Request{SystemPrompt: "SYSTEM", UserPrompt: "USER", ResponseSchema: json.RawMessage(wireSchema)}

	res, err := codex.NewRunner("gpt-5.5", "high").Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	args := recordedArgs(t, dir)
	for _, want := range [][]string{
		{"exec", "--json"}, {"--sandbox", "read-only"}, {"--ignore-user-config"}, {"--ignore-rules"}, {"--ephemeral"},
		{"-c", "project_doc_max_bytes=0"}, {"-m", "gpt-5.5"}, {"-c", `model_reasoning_effort="high"`},
	} {
		if !containsRun(args, want) {
			t.Errorf("args %q lack %q", args, want)
		}
	}
	if args[len(args)-1] != "-" {
		t.Errorf("the prompt should be read from stdin, got args %q", args)
	}
	if got := recorded(t, dir, "schema"); got != wireSchema {
		t.Errorf("--output-schema should carry the schema byte for byte, got %q", got)
	}
	if got := recorded(t, dir, "stdin"); got != req.Combined() {
		t.Errorf("stdin = %q, want the combined prompt", got)
	}
	if res.Text != `{"answer":"ok"}` {
		t.Errorf("text = %q, want the final agent message", res.Text)
	}
	if want := (llm.Usage{InputTokens: 120, CacheReadTokens: 20, OutputTokens: 15}); res.Usage != want {
		t.Errorf("usage = %+v, want %+v", res.Usage, want)
	}
	if res.Identity != (llm.Identity{Provider: "codex", Model: "gpt-5.5"}) {
		t.Errorf("identity = %+v", res.Identity)
	}
	cwd := strings.TrimSpace(recorded(t, dir, "cwd"))
	if _, err := os.Stat(cwd); !os.IsNotExist(err) {
		t.Errorf("the working directory %s should be the call's own and outlive it not (stat err %v)", cwd, err)
	}
}

func containsRun(args, run []string) bool {
	for i := range args {
		if i+len(run) <= len(args) && slices.Equal(args[i:i+len(run)], run) {
			return true
		}
	}
	return false
}

// An empty model and effort leave codex's own defaults: no -m and no
// model_reasoning_effort; a free-text request passes no schema.
func TestRunLeavesCodexDefaults(t *testing.T) {
	dir := fakeCLI(t, answered)
	if _, err := codex.NewRunner("", "").Run(context.Background(), llm.Request{UserPrompt: "U"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	args := recordedArgs(t, dir)
	if slices.Contains(args, "-m") || slices.Contains(args, "--output-schema") || strings.Contains(strings.Join(args, " "), "model_reasoning_effort") {
		t.Errorf("empty model and effort and no schema should pass none of them, got %q", args)
	}
}

// The child answers on the Codex login, never on a key or endpoint the
// invoking shell happens to export; the rest of the environment passes.
func TestRunLeavesInheritedCredentialsOut(t *testing.T) {
	dir := fakeCLI(t, answered)
	t.Setenv("OPENAI_API_KEY", "sk-inherited")
	t.Setenv("CODEX_API_KEY", "inherited")
	t.Setenv("OPENAI_BASE_URL", "https://inherited.example")
	t.Setenv("CODEX_HOME", "/home/codex")

	if _, err := codex.NewRunner("", "").Run(context.Background(), llm.Request{UserPrompt: "U"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	env := strings.Split(recorded(t, dir, "env"), "\n")
	for _, name := range []string{"OPENAI_API_KEY", "CODEX_API_KEY", "OPENAI_BASE_URL"} {
		if slices.ContainsFunc(env, func(entry string) bool { return strings.HasPrefix(entry, name+"=") }) {
			t.Errorf("an inherited credential should not reach the child, got %s", name)
		}
	}
	if !slices.Contains(env, "CODEX_HOME=/home/codex") || !slices.Contains(env, "PATH="+os.Getenv("PATH")) {
		t.Error("the child should keep the rest of the environment")
	}
}

func TestScrubbedEnv(t *testing.T) {
	env := []string{"PATH=/bin", "OPENAI_API_KEY=sk-1", "CODEX_API_KEY=c-1", "OPENAI_BASE_URL=https://x", "OPENAI_API_KEY_HINT=sk-look-alike", "OTHER_API_KEY=o-1"}

	got := codex.ScrubbedEnv(env)

	if want := []string{"PATH=/bin", "OPENAI_API_KEY_HINT=sk-look-alike", "OTHER_API_KEY=o-1"}; !slices.Equal(got, want) {
		t.Errorf("only the credential variables, matched by name, should go, got %q", got)
	}
}

func TestRunFailsLoud(t *testing.T) {
	for name, tc := range map[string]struct {
		events string
		req    llm.Request
		want   string
	}{
		"error event":   {`{"type":"error","message":"quota exceeded"}` + "\n", llm.Request{UserPrompt: "U"}, "quota exceeded"},
		"failed turn":   {`{"type":"turn.failed","error":{"message":"model refused"}}` + "\n", llm.Request{UserPrompt: "U"}, "model refused"},
		"no message":    {`{"type":"turn.completed","usage":{}}` + "\n", llm.Request{UserPrompt: "U"}, "no agent message"},
		"not JSON":      {`{"type":"item.completed","item":{"type":"agent_message","text":"plain"}}` + "\n", llm.Request{UserPrompt: "U", ResponseSchema: json.RawMessage(wireSchema)}, "no JSON for the response schema"},
		"broken stream": {`{"type":` + "\n", llm.Request{UserPrompt: "U"}, "parsing codex exec event"},
	} {
		t.Run(name, func(t *testing.T) {
			fakeCLI(t, tc.events)
			_, err := codex.NewRunner("", "").Run(context.Background(), tc.req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error naming %q, got %v", tc.want, err)
			}
		})
	}
}
