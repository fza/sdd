// Package testsupport holds support shared by the module's tests.
package testsupport

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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

var standIns = []string{"claude", "codex", "sdd"}

// Option adjusts what RunWithoutLiveLLMs refuses.
type Option func(*options)

type options struct {
	allowOllama bool
}

// AllowOllama lets requests to Ollama's default port through, for a live
// evaluation a developer opts into; the rest of the guard stays on.
func AllowOllama() Option {
	return func(o *options) { o.allowOllama = true }
}

// RunWithoutLiveLLMs runs a package's tests with the XDG base directories in
// a temporary directory, the user's and the system's git config ignored,
// claude, codex and sdd first on PATH as stand-ins that fail every call, and
// an HTTP transport that refuses Ollama's default port. It fails the run when
// a test reached a stand-in or Ollama: a live CLI runs the developer's own
// hooks, and those must never see the developer's real data. A test needing
// the sdd binary calls the one it built by path.
func RunWithoutLiveLLMs(m *testing.M, opts ...Option) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if dir := os.Getenv(guardDirEnv); dir != "" {
		err := checkInherited(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s=%s names no guard this process runs under: %v\n", guardDirEnv, dir, err)
			os.Exit(1)
		}
		o.refuseOllama(filepath.Join(dir, "bin", "calls"))
		os.Exit(m.Run())
	}
	dir, err := os.MkdirTemp("", "sdd-test-llm-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	bin := filepath.Join(dir, "bin")
	err = os.Mkdir(bin, 0o755)
	for _, name := range standIns {
		if err == nil {
			err = os.WriteFile(filepath.Join(bin, name), failingCLI, 0o755)
		}
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "gitconfig"), nil, 0o644)
	}
	env := guardEnv(dir)
	env["PATH"] = bin + string(os.PathListSeparator) + os.Getenv("PATH")
	for name, value := range env {
		if err == nil {
			err = os.Setenv(name, value)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	o.refuseOllama(filepath.Join(bin, "calls"))
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

// guardEnv is the environment a guarded run sets up in dir, PATH aside.
func guardEnv(dir string) map[string]string {
	env := map[string]string{
		guardDirEnv:           dir,
		"GIT_CONFIG_GLOBAL":   filepath.Join(dir, "gitconfig"),
		"GIT_CONFIG_NOSYSTEM": "1",
	}
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		env[name] = filepath.Join(dir, strings.ToLower(name))
	}
	return env
}

// checkInherited reports how this process's environment differs from the one
// the guarded run owning dir hands to the processes it starts. A test that
// changes PATH, git or XDG variables before starting a test binary gets that
// binary refused, and so does one running `go test`, which puts GOROOT/bin
// first on PATH.
func checkInherited(dir string) error {
	var errs []error
	bin := filepath.Join(dir, "bin")
	first, _, _ := strings.Cut(os.Getenv("PATH"), string(os.PathListSeparator))
	if first != bin {
		errs = append(errs, fmt.Errorf("PATH starts with %q, not %q", first, bin))
	}
	for _, name := range standIns {
		script, err := os.ReadFile(filepath.Join(bin, name))
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("stand-in %s: %w", name, err))
		case !bytes.Equal(script, failingCLI):
			errs = append(errs, fmt.Errorf("stand-in %s is not the guard's", name))
		}
	}
	env := guardEnv(dir)
	names := slices.Sorted(maps.Keys(env))
	for _, name := range names {
		if got := os.Getenv(name); got != env[name] {
			errs = append(errs, fmt.Errorf("%s is %q, not %q", name, got, env[name]))
		}
	}
	return errors.Join(errs...)
}

func (o options) refuseOllama(calls string) {
	if !o.allowOllama {
		http.DefaultTransport = &ollamaRefuser{next: http.DefaultTransport, calls: calls}
	}
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
