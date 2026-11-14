package cliapp_test

import (
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/cliapp"
)

// Help descriptions print as written, so a line past the terminal width
// wraps mid-word in a standard 80-column terminal.
const maxHelpLineWidth = 80

func TestCommandDescriptionsFitATerminalLine(t *testing.T) {
	var walk func(path string, cmd *cli.Command)
	walk = func(path string, cmd *cli.Command) {
		for i, line := range strings.Split(cmd.Description, "\n") {
			if width := len([]rune(line)); width > maxHelpLineWidth {
				t.Errorf("%s: description line %d should fit a terminal line, is %d wide: %q", path, i+1, width, line)
			}
		}
		for _, sub := range cmd.Commands {
			walk(path+" "+sub.Name, sub)
		}
	}
	walk("sdd", cliapp.New(cliapp.Options{}))
}
