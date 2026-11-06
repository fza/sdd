package claude_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llm/claude"
	"github.com/networkteam/sdd/pkg/llm"
)

// problemChars are the characters that broke the checks' JSON output before.
const problemChars = "„Code einlösen“ «zitiert» ‚einfach‘ “English” an ASCII \" quote, Options{Zebra: true}, C:\\Projekte\\de.json, ^Code\\s+[A-Z0-9]{8}$, a line\nbreak, <b> & 🎉"

const wireSchema = `{"type":"object","properties":{"reasoning":{"type":"string"},"axis":{"type":"string","enum":["form"]}},"required":["reasoning","axis"],"additionalProperties":false}`

// fakeCLI puts a claude executable on PATH that records its arguments and
// stdin into dir and prints response as the CLI's JSON output.
func fakeCLI(t *testing.T, response string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "response.json"), []byte(response), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do printf '%s\\0' \"$a\"; done > '" + dir + "/args'\n" +
		"cat > '" + dir + "/stdin'\n" +
		"cat '" + dir + "/response.json'\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func recordedArgs(t *testing.T, dir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
}

// A structured request hands the schema to --json-schema byte for byte and
// returns the structured output the CLI validated, problem characters
// unchanged both ways; the prompt goes to stdin and usage is reported.
func TestRunWithResponseSchema(t *testing.T) {
	answer := `{"reasoning":` + mustJSON(problemChars) + `,"axis":"form"}`
	dir := fakeCLI(t, `{"type":"result","result":"ignored when a schema is set","structured_output":`+answer+
		`,"total_cost_usd":0.25,"usage":{"input_tokens":11,"output_tokens":22,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}`)

	req := llm.Request{
		SystemPrompt:   "SYSTEM " + problemChars,
		UserPrompt:     "USER " + problemChars,
		ResponseSchema: json.RawMessage(wireSchema),
	}
	res, err := claude.NewRunner("sonnet").Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	args := recordedArgs(t, dir)
	i := slices.Index(args, "--json-schema")
	if i < 0 || i+1 >= len(args) || args[i+1] != wireSchema {
		t.Errorf("--json-schema not passed byte for byte: %q", args)
	}
	stdin, err := os.ReadFile(filepath.Join(dir, "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(stdin) != req.Combined() {
		t.Errorf("stdin = %q, want the combined prompt", stdin)
	}
	if !jsonEqual(t, []byte(res.Text), []byte(answer)) {
		t.Errorf("text = %q, want the structured output %q", res.Text, answer)
	}
	var decoded struct{ Reasoning string }
	if err := json.Unmarshal([]byte(res.Text), &decoded); err != nil || decoded.Reasoning != problemChars {
		t.Errorf("problem characters changed on the way back: %q (%v)", decoded.Reasoning, err)
	}
	want := llm.Usage{InputTokens: 11, OutputTokens: 22, CacheReadTokens: 3, CacheCreateTokens: 4, CostUSD: 0.25}
	if res.Usage != want {
		t.Errorf("usage = %+v, want %+v", res.Usage, want)
	}
}

// A CLI answer without structured output fails loudly rather than handing on
// the free-text result.
func TestRunWithResponseSchemaRequiresStructuredOutput(t *testing.T) {
	fakeCLI(t, `{"type":"result","result":"{\"reasoning\":\"r\"}","usage":{}}`)
	_, err := claude.NewRunner("sonnet").Run(context.Background(), llm.Request{UserPrompt: "U", ResponseSchema: json.RawMessage(wireSchema)})
	if err == nil || !strings.Contains(err.Error(), "no structured output") {
		t.Fatalf("want a missing structured output error, got %v", err)
	}
}

// A free-text request passes no schema and returns the CLI's result.
func TestRunWithoutResponseSchema(t *testing.T) {
	dir := fakeCLI(t, `{"type":"result","result":"SERVED","usage":{}}`)
	res, err := claude.NewRunner("sonnet").Run(context.Background(), llm.Request{UserPrompt: "U"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if slices.Contains(recordedArgs(t, dir), "--json-schema") {
		t.Error("free-text request passed --json-schema")
	}
	if res.Text != "SERVED" {
		t.Errorf("text = %q, want SERVED", res.Text)
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("decoding %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("decoding %s: %v", b, err)
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(ax) == string(by)
}

// What the child process actually sees is the whole point of the isolation, and
// it is not inferable from the adapter's own state. A stand-in binary on PATH
// reports its arguments, working directory and credential environment back
// through the response the runner parses.
func TestChildRunsIsolated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in binary is a shell script")
	}

	binDir := t.TempDir()
	script := filepath.Join(binDir, "claude")
	body := `#!/bin/sh
printf '{"result":"args=%s cwd=%s key=%s token=%s base=%s","usage":{"input_tokens":1,"output_tokens":2}}\n' \
  "$*" "$PWD" "${ANTHROPIC_API_KEY:-absent}" "${ANTHROPIC_AUTH_TOKEN:-absent}" "${ANTHROPIC_BASE_URL:-absent}"
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("writing stand-in binary: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-not-reach-the-child")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "should-not-reach-the-child")
	t.Setenv("ANTHROPIC_BASE_URL", "https://should-not-reach-the-child")

	invocationDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	res, err := claude.NewRunner("claude-sonnet-4-6").Run(context.Background(), llm.Request{UserPrompt: "x"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(res.Text, "--safe-mode") {
		t.Errorf("the child must run in safe mode, got %q", res.Text)
	}
	for _, leaked := range []string{"key=sk-should-not-reach-the-child", "token=should-not-reach-the-child", "base=https://should-not-reach-the-child"} {
		if strings.Contains(res.Text, leaked) {
			t.Errorf("credential reached the child: %q", res.Text)
		}
	}
	if !strings.Contains(res.Text, "key=absent") {
		t.Errorf("ANTHROPIC_API_KEY must be absent, not empty or set: %q", res.Text)
	}

	childDir := valueOf(res.Text, "cwd=")
	if childDir == "" {
		t.Fatalf("no working directory reported: %q", res.Text)
	}
	if sameDir(t, childDir, invocationDir) {
		t.Errorf("the child ran in the invocation's directory (%s), loading the project's own context", childDir)
	}
	if _, err := os.Stat(childDir); !os.IsNotExist(err) {
		t.Errorf("working directory %s outlived the call (stat err %v)", childDir, err)
	}
}

func valueOf(text, key string) string {
	_, rest, found := strings.Cut(text, key)
	if !found {
		return ""
	}
	value, _, _ := strings.Cut(rest, " ")
	return value
}

// A temporary directory resolves through symlinks on macOS, so the comparison
// is made on resolved paths.
func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	return ra == rb
}
