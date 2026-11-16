package hooks

import (
	"bytes"
	"context"
	"fmt"
	"os"
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
// declared in sources, read afresh on every call, and the allow record.
type Commands struct {
	sources Sources
	allows  *Allows
}

// New returns Commands over the hooks sources declares.
func New(sources Sources, allows *Allows) *Commands {
	return &Commands{sources: sources, allows: allows}
}

// Hooks returns the hooks as declared now, those that came with a repository
// marked NeedsAllow unless the allow record holds the repository's current
// hash.
func (c *Commands) Hooks(ctx context.Context) ([]model.Hook, error) {
	layers, err := declaredLayers(ctx, c.sources)
	if err != nil {
		return nil, err
	}
	for _, group := range gateGroups(layers) {
		hash, err := gateHash(group)
		if err != nil {
			return nil, err
		}
		allowed, err := c.allows.Allowed(group.gate.key, hash)
		if err != nil {
			return nil, err
		}
		for _, i := range group.layers {
			for j := range layers[i].hooks {
				layers[i].hooks[j].NeedsAllow = !allowed
			}
		}
	}
	var hooks []model.Hook
	for _, layer := range layers {
		hooks = append(hooks, layer.hooks...)
	}
	return hooks, nil
}

// Run runs hook's file with its args in its hooks folder, stdin on its
// standard input, and returns its stdout. When ctx ends, the hook's whole
// process group is killed.
func (c *Commands) Run(ctx context.Context, hook model.Hook, stdin []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, hook.Path, hook.Args...)
	cmd.Dir = hook.Origin.Dir
	cmd.Env = hookEnv()
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

// hookEnvAllowlist names the variables of sdd's environment a hook sees;
// everything else, such as API keys in the MCP server's environment, stays
// with sdd. The payload arrives on stdin, not in the environment.
var hookEnvAllowlist = []string{"PATH", "HOME", "LANG", "TMPDIR"}

func hookEnv() []string {
	env := []string{}
	for _, name := range hookEnvAllowlist {
		value, ok := os.LookupEnv(name)
		if ok {
			env = append(env, name+"="+value)
		}
	}
	return env
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
