package cliapp_test

import (
	"os"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

// openTerminal opens a pseudo-terminal: the controlling side a test types
// into, and the terminal side a command reads as its standard input.
func openTerminal(t *testing.T) (controller, terminal *os.File) {
	t.Helper()
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("open pseudo-terminal: %v", err)
	}
	controller = os.NewFile(uintptr(fd), "/dev/ptmx")
	t.Cleanup(func() { _ = controller.Close() })
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatalf("unlock pseudo-terminal: %v", err)
	}
	number, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatalf("name pseudo-terminal: %v", err)
	}
	return controller, openTerminalSide(t, "/dev/pts/"+strconv.Itoa(number))
}
