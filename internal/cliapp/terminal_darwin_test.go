package cliapp_test

import (
	"bytes"
	"os"
	"syscall"
	"testing"
	"unsafe"

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
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		t.Fatalf("grant pseudo-terminal: %v", err)
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		t.Fatalf("unlock pseudo-terminal: %v", err)
	}
	name := make([]byte, 128)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		t.Fatalf("name pseudo-terminal: %v", errno)
	}
	return controller, openTerminalSide(t, string(name[:bytes.IndexByte(name, 0)]))
}
