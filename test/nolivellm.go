// Package test holds support shared by the module's tests.
package test

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
var failingLLMCLI []byte

// ollamaPort is the port of Ollama's default endpoint, which sdd falls back to
// when a config names the ollama provider without an endpoint.
const ollamaPort = "11434"

// RunWithoutLiveLLMs runs a package's tests with the XDG base directories in
// a temporary directory, claude and codex CLIs first on PATH that fail every
// call and an HTTP transport that refuses Ollama's default port. It fails the
// run when a test reached a CLI or Ollama: a live CLI runs the developer's own
// hooks, and those must never see the developer's real data.
func RunWithoutLiveLLMs(m *testing.M) {
	dir, err := os.MkdirTemp("", "sdd-test-llm-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	bin := filepath.Join(dir, "bin")
	err = os.Mkdir(bin, 0o755)
	for _, name := range []string{"claude", "codex"} {
		if err == nil {
			err = os.WriteFile(filepath.Join(bin, name), failingLLMCLI, 0o755)
		}
	}
	if err == nil {
		err = os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		if err == nil {
			err = os.Setenv(name, filepath.Join(dir, strings.ToLower(name)))
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ollama := &ollamaRefuser{next: http.DefaultTransport}
	http.DefaultTransport = ollama
	code := m.Run()
	calls, err := os.ReadFile(filepath.Join(bin, "calls"))
	switch {
	case err == nil:
		fmt.Fprintf(os.Stderr, "tests started an LLM CLI; pass the input that needs no model call or a fake provider:\n%s", calls)
		code = 1
	case !errors.Is(err, fs.ErrNotExist):
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	if refused := ollama.refused(); refused != "" {
		fmt.Fprintf(os.Stderr, "tests called a live Ollama; give the config an ollama_endpoint served by a fake:\n%s", refused)
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
	mu    sync.Mutex
	calls []string
}

func (o *ollamaRefuser) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Port() != ollamaPort {
		return o.next.RoundTrip(req)
	}
	if req.Body != nil {
		_ = req.Body.Close()
	}
	call := req.Method + " " + req.URL.String()
	o.mu.Lock()
	o.calls = append(o.calls, call)
	o.mu.Unlock()
	return nil, fmt.Errorf("%s: no live Ollama runs in tests", call)
}

func (o *ollamaRefuser) refused() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.calls) == 0 {
		return ""
	}
	return strings.Join(o.calls, "\n") + "\n"
}
