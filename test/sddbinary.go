package test

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/cliapp"
)

//go:embed fixtures/nolivellm/cli.sh
var failingLLMCLI []byte

// RunTestsAsSDD runs a package's tests, or, when git calls the test binary as
// one of the merge drivers the sdd-owned graph clone passes per call (the
// running binary names itself there), runs that sdd command instead. The
// tests find claude, codex and sdd on PATH failing every call, so a model
// call sdd falls back to never reaches a live model, and any such call fails
// the package run.
func RunTestsAsSDD(m *testing.M) {
	if len(os.Args) > 1 && (os.Args[1] == "merge-entry" || os.Args[1] == "merge-config") {
		app := cliapp.New(cliapp.Options{Version: "test", Reader: os.Stdin, Writer: os.Stdout, ErrWriter: os.Stderr})
		err := app.Run(context.Background(), append([]string{"sdd"}, os.Args[1:]...))
		var exit cli.ExitCoder
		switch {
		case err == nil:
			os.Exit(0)
		case errors.As(err, &exit):
			os.Exit(exit.ExitCode())
		default:
			os.Exit(1)
		}
	}
	dir, err := os.MkdirTemp("", "sdd-test-llm-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, name := range []string{"claude", "codex", "sdd"} {
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, name), failingLLMCLI, 0o755)
		}
	}
	if err == nil {
		err = os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	// A test overriding only some XDG directories keeps the rest here, not
	// in the developer's own, whose clones a cache prune would judge.
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		if err == nil {
			err = os.Setenv(name, filepath.Join(dir, strings.ToLower(strings.TrimPrefix(name, "XDG_"))))
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	switch {
	case err == nil:
		fmt.Fprintf(os.Stderr, "tests started an LLM CLI or the installed sdd; pass the input that needs neither, or a fake:\n%s", calls)
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
