// Package testsupport holds support shared by the module's tests.
package testsupport

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

//go:embed fixtures/nolivellm/cli.sh
var failingCLI []byte

// ollamaPort is the port of Ollama's default endpoint, which sdd falls back to
// when a config names the ollama provider without an endpoint.
const ollamaPort = "11434"

// guardDirEnv names the guard's directory to the test binaries a guarded run
// starts, so they record into it instead of setting up a guard of their own:
// a child killed mid-run then leaves no directory and loses no call.
const guardDirEnv = "SDD_TEST_GUARD_DIR"

// RunWithoutLiveLLMs runs a package's tests with the XDG base directories in
// a temporary directory, the user's and the system's git config ignored,
// claude, codex and sdd first on PATH as stand-ins that fail every call, and
// an HTTP transport that refuses Ollama's default port. It fails the run when
// a test reached a stand-in or Ollama: a live CLI runs the developer's own
// hooks, and those must never see the developer's real data. A test needing
// the sdd binary calls the one it built by path.
func RunWithoutLiveLLMs(m *testing.M) {
	if dir := os.Getenv(guardDirEnv); dir != "" {
		http.DefaultTransport = &ollamaRefuser{next: http.DefaultTransport, calls: filepath.Join(dir, "bin", "calls")}
		os.Exit(m.Run())
	}
	dir, err := os.MkdirTemp("", "sdd-test-llm-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	bin := filepath.Join(dir, "bin")
	err = os.Mkdir(bin, 0o755)
	for _, name := range []string{"claude", "codex", "sdd"} {
		if err == nil {
			err = os.WriteFile(filepath.Join(bin, name), failingCLI, 0o755)
		}
	}
	gitConfig := filepath.Join(dir, "gitconfig")
	if err == nil {
		err = os.WriteFile(gitConfig, nil, 0o644)
	}
	env := map[string]string{
		guardDirEnv:           dir,
		"PATH":                bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL":   gitConfig,
		"GIT_CONFIG_NOSYSTEM": "1",
	}
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		env[name] = filepath.Join(dir, strings.ToLower(name))
	}
	for name, value := range env {
		if err == nil {
			err = os.Setenv(name, value)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	http.DefaultTransport = &ollamaRefuser{next: http.DefaultTransport, calls: filepath.Join(bin, "calls")}
	code := m.Run()
	calls, err := os.ReadFile(filepath.Join(bin, "calls"))
	switch {
	case err == nil:
		fmt.Fprintf(os.Stderr, "tests reached an installed CLI or a live Ollama; pass the input that needs no model call, a fake provider, an ollama_endpoint served by a fake or the built binary's path:\n%s", calls)
		code = 1
	case !errors.Is(err, fs.ErrNotExist):
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	err = os.RemoveAll(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

type ollamaRefuser struct {
	next  http.RoundTripper
	calls string
	mu    sync.Mutex
}

func (o *ollamaRefuser) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Port() != ollamaPort {
		return o.next.RoundTrip(req)
	}
	if req.Body != nil {
		_ = req.Body.Close()
	}
	call := "ollama " + req.Method + " " + req.URL.String()
	refused := fmt.Errorf("%s: no live Ollama runs in tests", call)
	err := o.record(call)
	if err != nil {
		return nil, errors.Join(refused, err)
	}
	return nil, refused
}

func (o *ollamaRefuser) record(call string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	f, err := os.OpenFile(o.calls, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(f, call)
	return errors.Join(err, f.Close())
}
