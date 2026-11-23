package claude_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llm/claude"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

func pwdStandIn() {
	report := "PWD names another directory: " + os.Getenv("PWD")
	pwd, errPWD := os.Stat(os.Getenv("PWD"))
	cwd, errCwd := os.Stat(".")
	if errPWD == nil && errCwd == nil && os.SameFile(pwd, cwd) {
		report = "PWD names the working directory"
	}
	answer, _ := json.Marshal(map[string]string{"result": report})
	_, _ = os.Stdout.Write(answer)
}

// problemChars are the characters that broke the checks' JSON output before.
const problemChars = "„Code einlösen“ «zitiert» ‚einfach‘ “English” an ASCII \" quote, Options{Zebra: true}, C:\\Projekte\\de.json, ^Code\\s+[A-Z0-9]{8}$, a line\nbreak, <b> & 🎉"

const wireSchema = `{"type":"object","properties":{"reasoning":{"type":"string"},"axis":{"type":"string","enum":["form"]}},"required":["reasoning","axis"],"additionalProperties":false}`

// fakeCLI puts a claude executable on PATH that records its arguments,
// environment and stdin into dir and prints response as the CLI's JSON output.
func fakeCLI(t *testing.T, response string) string {
	t.Helper()
	return fakeCLIExiting(t, response, "", 0)
}

// fakeCLIExiting is fakeCLI that also records its working directory, prints
// stderr and exits with code.
func fakeCLIExiting(t *testing.T, response, stderr string, code int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in binary is a shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "response.json"), []byte(response), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stderr"), []byte(stderr), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do printf '%s\\0' \"$a\"; done > '" + dir + "/args'\n" +
		"env > '" + dir + "/env'\n" +
		"printf '%s' \"$(pwd -P)\" > '" + dir + "/cwd'\n" +
		"cat > '" + dir + "/stdin'\n" +
		"cat '" + dir + "/response.json'\n" +
		"cat '" + dir + "/stderr' >&2\n" +
		"exit " + strconv.Itoa(code) + "\n"
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

func recordedEnvNames(t *testing.T, dir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "env"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for line := range strings.Lines(string(raw)) {
		if name, _, found := strings.Cut(line, "="); found {
			names = append(names, name)
		}
	}
	return names
}

// Login mode closes Claude's own name space but for the login's variables and
// a few TLS and path ones, and loads no settings file, whose env block and
// apiKeyHelper apply even in safe mode. Env mode passes it all. Neither mode
// passes the markers of a Claude Code session that started sdd.
func TestRunCredentials(t *testing.T) {
	classes := []struct {
		class          string
		names          []string
		inLogin, inEnv bool
	}{
		{"Anthropic keys, endpoints and profile", []string{"ANTHROPIC_API_KEY", "ANTHROPIC_VERTEX_BASE_URL", "ANTHROPIC_PROFILE"}, false, true},
		{"provider switches", []string{"CLAUDE_CODE_USE_FOUNDRY"}, false, true},
		{"Claude Code credential hand-offs", []string{"CLAUDE_CODE_API_KEY_FILE_DESCRIPTOR", "CLAUDE_CODE_HOST_CREDS_FILE", "CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST", "CLAUDE_CODE_API_BASE_URL", "CLAUDE_CODE_OAUTH_REFRESH_TOKEN"}, false, true},
		{"names in any case, as Windows reads them", []string{"Anthropic_Api_Key", "claude_code_use_vertex"}, false, true},
		{"the login's own", []string{"CLAUDE_CONFIG_DIR", "ANTHROPIC_CONFIG_DIR", "Anthropic_Config_Dir", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR", "CLAUDE_SECURESTORAGE_CONFIG_DIR"}, true, true},
		{"TLS and paths", []string{"CLAUDE_CODE_CERT_STORE", "CLAUDE_CODE_CLIENT_CERT", "CLAUDE_CODE_CLIENT_KEY", "CLAUDE_CODE_CLIENT_KEY_PASSPHRASE", "CLAUDE_CODE_DISABLE_LOOPED_CROSS_CERT_DROP", "CLAUDE_CODE_DISABLE_SELF_NAMED_CERT_DROP", "CLAUDE_CODE_GIT_BASH_PATH", "CLAUDE_CODE_TMPDIR"}, true, true},
		{"outside Claude's name space", []string{"HTTPS_PROXY", "ALL_PROXY", "NODE_USE_SYSTEM_CA", "SystemRoot", "AWS_ACCESS_KEY_ID"}, true, true},
		{"parent session markers", []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_SESSION_ATTENDED", "CLAUDE_PID", "CLAUDE_EFFORT", "CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_EXECPATH"}, false, false},
	}
	for _, credentials := range []model.ClaudeCLICredentials{model.ClaudeCLICredentialsLogin, model.ClaudeCLICredentialsEnv} {
		t.Run(string(credentials), func(t *testing.T) {
			dir := fakeCLI(t, `{"type":"result","result":"ok","usage":{}}`)
			for _, c := range classes {
				for _, name := range c.names {
					t.Setenv(name, "C")
				}
			}
			if _, err := claude.NewRunner("sonnet", "", credentials).Run(context.Background(), llm.Request{UserPrompt: "U"}); err != nil {
				t.Fatalf("Run: %v", err)
			}
			login := credentials == model.ClaudeCLICredentialsLogin
			got := recordedEnvNames(t, dir)
			for _, c := range classes {
				want := c.inEnv
				if login {
					want = c.inLogin
				}
				for _, name := range c.names {
					if slices.Contains(got, name) != want {
						t.Errorf("%s (%s): reaching the child should be %v", name, c.class, want)
					}
				}
			}
			sources, found := flagValue(recordedArgs(t, dir), "--setting-sources")
			if loadsSettings := !found || sources != ""; loadsSettings == login {
				t.Errorf("settings files should load only with credentials env, got %q", recordedArgs(t, dir))
			}
		})
	}
}

// A failed call says what the CLI reported, on stdout or stderr, and a login
// failure in login mode names the ways to authenticate.
func TestRunReportsCLIFailure(t *testing.T) {
	const loginHint = "llm.claude_cli.credentials: env"
	for _, tc := range []struct {
		name        string
		credentials model.ClaudeCLICredentials
		stdout      string
		stderr      string
		code        int
		reported    string
		whole       bool
		hinted      bool
	}{
		{
			name:        "signed out",
			credentials: model.ClaudeCLICredentialsLogin,
			stdout:      `{"type":"result","is_error":true,"result":"Not logged in · Please run /login"}`,
			code:        1,
			reported:    "Not logged in · Please run /login",
			hinted:      true,
		},
		{
			name:        "rejected credential",
			credentials: model.ClaudeCLICredentialsLogin,
			stdout:      `{"type":"result","is_error":true,"result":"API Error: 401 {\"type\":\"error\",\"error\":{\"type\":\"authentication_error\"}}"}`,
			code:        1,
			reported:    "authentication_error",
			hinted:      true,
		},
		{
			name:        "signed out with credentials env",
			credentials: model.ClaudeCLICredentialsEnv,
			stdout:      `{"type":"result","is_error":true,"result":"Not logged in · Please run /login"}`,
			code:        1,
			reported:    "Not logged in · Please run /login",
		},
		{
			name:        "unknown option",
			credentials: model.ClaudeCLICredentialsLogin,
			stderr:      "error: unknown option '--safe-mode'\n",
			code:        1,
			reported:    "error: unknown option '--safe-mode'",
		},
		{
			name:        "error result on a clean exit",
			credentials: model.ClaudeCLICredentialsLogin,
			stdout:      `{"type":"result","subtype":"success","is_error":true,"result":"API Error: 500 overloaded"}`,
			reported:    "claude -p: reported an error: API Error: 500 overloaded",
			whole:       true,
		},
		{
			name:        "message in errors",
			credentials: model.ClaudeCLICredentialsLogin,
			stdout:      `{"type":"result","subtype":"error_max_structured_output_retries","is_error":true,"errors":["Failed to provide valid structured output after 5 attempts"]}`,
			code:        1,
			reported:    "Failed to provide valid structured output after 5 attempts",
		},
		{
			name:        "login failure in errors",
			credentials: model.ClaudeCLICredentialsLogin,
			stdout:      `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["Not logged in · Please run /login"]}`,
			code:        1,
			reported:    "Not logged in · Please run /login",
			hinted:      true,
		},
		{
			name:        "subtype only",
			credentials: model.ClaudeCLICredentialsLogin,
			stdout:      `{"type":"result","subtype":"error_max_turns","is_error":true}`,
			code:        1,
			reported:    "claude -p: exit status 1: error_max_turns",
			whole:       true,
		},
		{
			name:        "crash after an answer",
			credentials: model.ClaudeCLICredentialsLogin,
			stdout:      `{"type":"result","subtype":"success","is_error":false,"result":"an answer"}`,
			stderr:      "crashed\n",
			code:        1,
			reported:    "claude -p: exit status 1: crashed",
			whole:       true,
		},
		{
			name:        "silent exit",
			credentials: model.ClaudeCLICredentialsLogin,
			code:        1,
			reported:    "claude -p: exit status 1",
			whole:       true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeCLIExiting(t, tc.stdout, tc.stderr, tc.code)
			_, err := claude.NewRunner("sonnet", "", tc.credentials).Run(context.Background(), llm.Request{UserPrompt: "U"})
			if err == nil {
				t.Fatal("a call the CLI reports as failed should fail")
			}
			if tc.whole && err.Error() != tc.reported {
				t.Errorf("the error should say what the CLI reported and nothing more, got %q", err)
			}
			if !strings.Contains(err.Error(), tc.reported) {
				t.Errorf("the error should carry what the CLI reported, got %q", err)
			}
			if strings.Contains(err.Error(), loginHint) != tc.hinted {
				t.Errorf("only a login failure in login mode should name the credentials setting, got %q", err)
			}
		})
	}
}

func TestRunRefusesOutputThatIsNotJSON(t *testing.T) {
	fakeCLI(t, "plain text")
	_, err := claude.NewRunner("sonnet", "", model.DefaultClaudeCLICredentials).Run(context.Background(), llm.Request{UserPrompt: "U"})
	if err == nil || !strings.Contains(err.Error(), "parsing claude JSON response") {
		t.Fatalf("output that is not JSON should fail as such, got %v", err)
	}
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
	res, err := claude.NewRunner("sonnet", "", model.DefaultClaudeCLICredentials).Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	args := recordedArgs(t, dir)
	if schema, _ := flagValue(args, "--json-schema"); schema != wireSchema {
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
	_, err := claude.NewRunner("sonnet", "", model.DefaultClaudeCLICredentials).Run(context.Background(), llm.Request{UserPrompt: "U", ResponseSchema: json.RawMessage(wireSchema)})
	if err == nil || !strings.Contains(err.Error(), "no structured output") {
		t.Fatalf("want a missing structured output error, got %v", err)
	}
}

// A free-text request passes no schema and returns the CLI's result.
func TestRunWithoutResponseSchema(t *testing.T) {
	dir := fakeCLI(t, `{"type":"result","result":"SERVED","usage":{}}`)
	res, err := claude.NewRunner("sonnet", "", model.DefaultClaudeCLICredentials).Run(context.Background(), llm.Request{UserPrompt: "U"})
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

// Each flag keeps something out of the call: the built-in tools, the configured
// MCP servers, the user's customizations, a transcript holding the draft.
func TestRunIsolationFlags(t *testing.T) {
	dir := fakeCLI(t, `{"type":"result","result":"ok","usage":{}}`)
	if _, err := claude.NewRunner("sonnet", "", model.DefaultClaudeCLICredentials).Run(context.Background(), llm.Request{UserPrompt: "U"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	args := recordedArgs(t, dir)
	for _, flag := range []string{"--strict-mcp-config", "--safe-mode", "--no-session-persistence"} {
		if !slices.Contains(args, flag) {
			t.Errorf("claude -p should run with %s, got %q", flag, args)
		}
	}
	if value, found := flagValue(args, "--tools"); !found || value != "" {
		t.Errorf("claude -p should run with no built-in tools, got %q", args)
	}
}

func flagValue(args []string, flag string) (string, bool) {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return "", false
	}
	return args[i+1], true
}

// The CLI reads PWD for its directory; an inherited one would name the
// project the call started in.
func TestChildPWDNamesItsDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a symlink to the test binary")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(self, filepath.Join(dir, "claude")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PWD", t.TempDir())

	res, err := claude.NewRunner("sonnet", "", model.DefaultClaudeCLICredentials).Run(context.Background(), llm.Request{UserPrompt: "U"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "PWD names the working directory" {
		t.Errorf("the child's PWD should name its own working directory, got %q", res.Text)
	}
}

// The child runs in a directory of its own, away from the project the call
// started in, and the directory goes with the call, failed or not.
func TestChildRunsIsolated(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	// The stand-in records its physical directory, so the invocation's is
	// compared resolved too.
	invocationDir, err := filepath.EvalSymlinks(wd)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		code int
	}{
		{"answered", 0},
		{"failed", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fakeCLIExiting(t, `{"type":"result","result":"ok","usage":{}}`, "", tc.code)
			_, _ = claude.NewRunner("sonnet", "", model.DefaultClaudeCLICredentials).Run(context.Background(), llm.Request{UserPrompt: "U"})

			raw, err := os.ReadFile(filepath.Join(dir, "cwd"))
			if err != nil {
				t.Fatal(err)
			}
			childDir := string(raw)
			if childDir == invocationDir {
				t.Errorf("the child ran in the invocation's directory (%s), loading the project's own context", childDir)
			}
			if _, err := os.Stat(childDir); !os.IsNotExist(err) {
				t.Errorf("working directory %s outlived the call (stat err %v)", childDir, err)
			}
		})
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

// The effort reaches --effort as written; none leaves the flag out.
func TestRunPassesEffort(t *testing.T) {
	dir := fakeCLI(t, `{"type":"result","result":"SERVED","usage":{}}`)
	if _, err := claude.NewRunner("sonnet", "high", model.DefaultClaudeCLICredentials).Run(context.Background(), llm.Request{UserPrompt: "U"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	args := recordedArgs(t, dir)
	if i := slices.Index(args, "--effort"); i < 0 || i+1 >= len(args) || args[i+1] != "high" {
		t.Errorf("--effort high not passed: %q", args)
	}
	if _, err := claude.NewRunner("sonnet", "", model.DefaultClaudeCLICredentials).Run(context.Background(), llm.Request{UserPrompt: "U"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if slices.Contains(recordedArgs(t, dir), "--effort") {
		t.Errorf("an empty effort should pass no --effort: %q", recordedArgs(t, dir))
	}
}
