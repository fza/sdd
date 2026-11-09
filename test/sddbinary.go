package test

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/cliapp"
)

//go:embed fixtures/noliveclaude/claude.sh
var failingClaude []byte

// RunTestsAsSDD runs a package's tests, or, when git calls the test binary as
// one of the merge drivers the sdd-owned graph clone passes per call (the
// running binary names itself there), runs that sdd command instead. The
// tests find a claude CLI on PATH that fails every call, so a model call
// sdd falls back to never reaches a live model.
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
	dir, err := os.MkdirTemp("", "sdd-test-claude-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	err = os.WriteFile(filepath.Join(dir, "claude"), failingClaude, 0o755)
	if err == nil {
		err = os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	err = os.RemoveAll(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}
