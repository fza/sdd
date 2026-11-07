package test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/cliapp"
)

// RunTestsAsSDD runs a package's tests, or, when git calls the test binary as
// one of the merge drivers the sdd-owned graph clone passes per call (the
// running binary names itself there), runs that sdd command instead.
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
	os.Exit(m.Run())
}
