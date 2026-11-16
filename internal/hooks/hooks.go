package hooks

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
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
// process group is killed. It reads the hooks again first and refuses one
// that no longer is as it was read or no longer allowed, so a change landing
// between reading the hooks and running one does not run.
func (c *Commands) Run(ctx context.Context, hook model.Hook, stdin []byte) ([]byte, error) {
	err := c.unchanged(ctx, hook)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, hook.Path, hook.Args...)
	cmd.Dir = hook.Origin.Dir
	cmd.Env = hookEnv()
	cmd.Stdin = bytes.NewReader(stdin)
	stdout, stderr := &cappedBuffer{limit: stdoutCap}, &cappedBuffer{limit: stderrCap}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = waitDelay
	err = cmd.Run()
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

// unchanged reports an error unless the hook is still declared as it was
// read and, when it came with the repository, still allowed.
func (c *Commands) unchanged(ctx context.Context, hook model.Hook) error {
	current, err := c.Hooks(ctx)
	if err != nil {
		return err
	}
	for _, now := range current {
		if now.Origin.ConfigPath != hook.Origin.ConfigPath || now.Origin.Index != hook.Origin.Index {
			continue
		}
		switch {
		case now.NeedsAllow:
			return fmt.Errorf("%s changed since the call read it and is not allowed as it is now; review it and run `sdd hooks allow`", hook.Label())
		case now.Path != hook.Path || !slices.Equal(now.Args, hook.Args) || now.Event != hook.Event:
			return fmt.Errorf("%s changed since the call read it; call again", hook.Label())
		}
		return nil
	}
	return fmt.Errorf("%s is no longer declared; call again", hook.Label())
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
