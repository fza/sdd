package hooks

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// waitDelay bounds how long a stopped hook's leftover children may hold its
// output open.
const waitDelay = time.Second

// stdoutCap and stderrCap bound what sdd keeps of a hook's output: stdout
// beyond its cap fails the hook, stderr beyond its cap is cut from the error.
const (
	stdoutCap = 1 << 20
	stderrCap = 4 << 10
)

// Commands implements the application's HookCommands port over the hooks
// declared in sources, read afresh on every call.
type Commands struct {
	sources Sources
}

// New returns Commands over the hooks sources declares.
func New(sources Sources) *Commands {
	return &Commands{sources: sources}
}

// Hooks returns the hooks as declared now.
func (c *Commands) Hooks(context.Context) ([]model.Hook, error) {
	return Declared(c.sources)
}

// Run runs hook's file with its args in its hooks folder, stdin on its
// standard input, and returns its stdout. When ctx ends, the hook's whole
// process group is killed.
func (c *Commands) Run(ctx context.Context, hook model.Hook, stdin []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, hook.Path, hook.Args...)
	cmd.Dir = hook.Origin.Dir
	cmd.Stdin = bytes.NewReader(stdin)
	stdout, stderr := &cappedBuffer{limit: stdoutCap}, &cappedBuffer{limit: stderrCap}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if stderr.over {
			message += " … (stderr cut)"
		}
		if message != "" {
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		return nil, err
	}
	if stdout.over {
		return nil, fmt.Errorf("wrote more than 1 MiB to stdout")
	}
	return stdout.Bytes(), nil
}

// cappedBuffer keeps up to limit bytes and drops the rest, so a hook writing
// without end still runs to its exit or timeout without growing sdd's memory.
// It wraps the buffer rather than embedding it, so io.Copy cannot bypass
// Write through the buffer's ReadFrom.
type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	room := b.limit - b.buf.Len()
	if len(p) > room {
		b.over = true
		b.buf.Write(p[:max(room, 0)])
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *cappedBuffer) String() string { return b.buf.String() }
