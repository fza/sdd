package hooks

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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

// headLines bounds the head of a command file a review shows.
const headLines = 20

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

// gateState is one repository's gate as it is now.
type gateState struct {
	gate   *repoGate
	hash   string
	files  []model.HookFile
	state  model.HookState
	notice string
	err    error
}

// resolve returns the hooks as declared now, each with its state, and the
// state of each repository's gate. A gate sdd cannot hash stays pending
// with the reason as its notice: it fails closed, never open.
func (c *Commands) resolve(ctx context.Context) ([]model.Hook, []gateState, error) {
	hooks, gates, err := declare(c.sources)
	if err != nil {
		return nil, nil, err
	}
	states := make([]gateState, 0, len(gates))
	byKey := map[string]gateState{}
	for _, gate := range gates {
		state := gateState{gate: gate, state: model.HookPending}
		state.hash, state.files, state.err = gateHash(ctx, c.sources.Tracker, gate)
		if state.err != nil {
			state.notice = fmt.Sprintf("sdd cannot check the hooks of %s, so they cannot run or be allowed: %v", gate.root, state.err)
		} else {
			state.state, state.notice = c.allows.State(gate.key, state.hash)
		}
		states = append(states, state)
		byKey[gate.key] = state
	}
	for i := range hooks {
		hooks[i].State = model.HookAllowed
		if !hooks[i].Origin.NeedsAllow() {
			continue
		}
		state := byKey[hooks[i].Origin.Checkout]
		hooks[i].State, hooks[i].Hash, hooks[i].Notice = state.state, state.hash, state.notice
	}
	return hooks, states, nil
}

// Hooks returns the hooks as declared now, each with its state.
func (c *Commands) Hooks(ctx context.Context) ([]model.Hook, error) {
	hooks, _, err := c.resolve(ctx)
	return hooks, err
}

// Reviews returns what the person sees before allowing each repository
// whose hooks are not allowed as they are now. A repository sdd cannot hash
// is an error: its hooks cannot be allowed.
func (c *Commands) Reviews(ctx context.Context) ([]model.HookReview, error) {
	_, states, err := c.resolve(ctx)
	if err != nil {
		return nil, err
	}
	var reviews []model.HookReview
	for _, state := range states {
		if state.state == model.HookAllowed {
			continue
		}
		if state.err != nil {
			return nil, errors.New(state.notice)
		}
		review := model.HookReview{
			Repo: state.gate.repo, Checkout: state.gate.key, State: state.state,
			Files: state.files, Heads: map[string][]string{}, Hash: state.hash,
		}
		for _, hook := range state.gate.hooks {
			hook.State, hook.Hash, hook.Notice = state.state, state.hash, state.notice
			review.Hooks = append(review.Hooks, hook)
			if _, ok := review.Heads[hook.Path]; ok {
				continue
			}
			if review.Heads[hook.Path], err = commandHead(hook.Path); err != nil {
				return nil, err
			}
		}
		reviews = append(reviews, review)
	}
	return reviews, nil
}

// Allow records each reviewed repository's hooks as allowed, refusing all of
// them when any changed since the person saw its review, so only what the
// person saw is ever allowed.
func (c *Commands) Allow(ctx context.Context, reviews []model.HookReview) error {
	_, states, err := c.resolve(ctx)
	if err != nil {
		return err
	}
	current := map[string]string{}
	for _, state := range states {
		if state.err == nil {
			current[state.gate.key] = state.hash
		}
	}
	hashes := map[string]string{}
	for _, review := range reviews {
		if review.Hash == "" || current[review.Checkout] != review.Hash {
			return fmt.Errorf("the hooks of %s changed since they were shown; review them again", review.Checkout)
		}
		hashes[review.Checkout] = review.Hash
	}
	return c.allows.Record(hashes)
}

// Run runs hook's file with its args in its hooks folder, stdin on its
// standard input, and returns its stdout. It reads the hooks again first and
// refuses one that no longer is as it was read or no longer allowed, so a
// change landing between reading the hooks and running one does not run.
// When ctx ends, the hook's whole process group is killed.
func (c *Commands) Run(ctx context.Context, hook model.Hook, stdin []byte) ([]byte, error) {
	if err := c.unchanged(ctx, hook); err != nil {
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
	if err := cmd.Run(); err != nil {
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
		return nil, errors.New("wrote more than 1 MiB to stdout")
	}
	return stdout.Bytes(), nil
}

// stdoutCap and stderrCap bound what sdd keeps of a hook's output: stdout
// beyond its cap fails the hook, stderr beyond its cap is cut from the error.
const (
	stdoutCap = 1 << 20
	stderrCap = 4 << 10
)

// hookEnvAllowlist names the variables of sdd's environment a hook sees;
// everything else, such as API keys in the MCP server's environment, stays
// with sdd. The payload arrives on stdin, not in the environment.
var hookEnvAllowlist = []string{"PATH", "HOME", "LANG", "LC_ALL", "LC_CTYPE", "TMPDIR", "USER", "LOGNAME"}

func hookEnv() []string {
	env := []string{}
	for _, name := range hookEnvAllowlist {
		if value, ok := os.LookupEnv(name); ok {
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

// unchanged reports an error unless the hook is still declared as it was
// read and still allowed.
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
		case now.State != model.HookAllowed:
			return fmt.Errorf("%s changed while the call ran and is not allowed as it is now; review it and run `sdd hooks allow`", hook.Label())
		case now.Path != hook.Path || !slices.Equal(now.Args, hook.Args) || now.Event != hook.Event || now.Hash != hook.Hash:
			return fmt.Errorf("%s changed while the call ran; call again", hook.Label())
		}
		return nil
	}
	return fmt.Errorf("%s is no longer declared; call again", hook.Label())
}

// commandHead returns the first lines of a text command file, or a line
// naming a binary one.
func commandHead(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	peek, _ := reader.Peek(8192)
	if bytes.IndexByte(peek, 0) >= 0 {
		info, err := file.Stat()
		if err != nil {
			return nil, err
		}
		return []string{fmt.Sprintf("(binary file, %d bytes)", info.Size())}, nil
	}
	var head []string
	scanner := bufio.NewScanner(reader)
	for len(head) < headLines && scanner.Scan() {
		head = append(head, strings.TrimRight(scanner.Text(), "\r"))
	}
	return head, scanner.Err()
}
