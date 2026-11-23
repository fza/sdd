package git

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// gitReadMaxBytes caps what one git read returns; a read past it is refused
// with a hint to narrow it.
const gitReadMaxBytes = 2 << 20

// gitReadOptions keep a read from running a program the repository's config
// or attributes name, from asking for input and from writing: no pager, no
// filesystem monitor hook, no signature check, no optional lock.
var gitReadOptions = []string{"--no-pager", "-c", "core.fsmonitor=false", "-c", "log.showSignature=false", "-c", "core.pager=cat"}

// gitReadEnv pins what a read's environment could otherwise change: pathspecs
// match literally, git takes no optional lock and asks for no credentials.
var gitReadEnv = []string{"GIT_LITERAL_PATHSPECS=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat"}

// gitReadTimeout caps one git read, a backstop on top of the terminal detach,
// as commitTimeout is for the auto-commit.
const gitReadTimeout = 30 * time.Second

// ReadGit runs one read of the repository's history and returns its text.
// sdd builds every command itself from the request; git runs detached from
// any terminal, bounded by ctx and gitReadTimeout, with external diff and
// textconv programs off. A request the repository cannot serve is refused
// with an error matching model.ErrGitReadRefused.
func (c CLI) ReadGit(ctx context.Context, request model.GitReadRequest) (string, error) {
	err := request.Validate()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, gitReadTimeout)
	defer cancel()
	switch request.Op {
	case model.GitReadFile:
		return c.readFileAt(ctx, request.Rev, request.Path)
	case model.GitReadLog:
		return c.readLog(ctx, request)
	case model.GitReadCommit:
		commit, err := c.resolveCommit(ctx, request.Rev)
		if err != nil {
			return "", err
		}
		return c.readText(ctx, "show", "--no-ext-diff", "--no-textconv", "--no-color", "--no-show-signature", "--stat=200", "--format=fuller", commit, "--")
	case model.GitReadDiff:
		from, err := c.resolveCommit(ctx, request.From)
		if err != nil {
			return "", err
		}
		to, err := c.resolveCommit(ctx, request.To)
		if err != nil {
			return "", err
		}
		return c.readText(ctx, withPath([]string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", from, to, "--"}, request.Path)...)
	}
	return "", model.GitReadRefusal(fmt.Sprintf("op %q has no read", request.Op))
}

func (c CLI) readFileAt(ctx context.Context, rev, path string) (string, error) {
	commit, err := c.resolveCommit(ctx, rev)
	if err != nil {
		return "", err
	}
	listing, err := c.readOutput(ctx, "ls-tree", "-l", "-z", commit, "--", path)
	if err != nil {
		return "", err
	}
	// One record, "<mode> <type> <object> <size>\t<path>"; none when the
	// revision holds nothing at path.
	meta, _, found := strings.Cut(strings.TrimSuffix(string(listing), "\x00"), "\t")
	fields := strings.Fields(meta)
	if !found || len(fields) != 4 {
		return "", model.GitReadMissing(fmt.Sprintf("revision %s holds no file %s", rev, path))
	}
	if fields[1] != "blob" {
		return "", model.GitReadRefusal(fmt.Sprintf("%s at revision %s is no file", path, rev))
	}
	size, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return "", fmt.Errorf("reading the size of %s at %s: %w", path, rev, err)
	}
	if size > gitReadMaxBytes {
		return "", model.GitReadRefusal(fmt.Sprintf("%s at revision %s holds %d bytes, more than one read returns (%d)", path, rev, size, gitReadMaxBytes))
	}
	content, err := c.readOutput(ctx, "cat-file", "blob", fields[2])
	if err != nil {
		return "", err
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return "", model.GitReadRefusal(fmt.Sprintf("%s at revision %s is a binary file", path, rev))
	}
	return string(content), nil
}

func (c CLI) readLog(ctx context.Context, request model.GitReadRequest) (string, error) {
	start := request.Rev
	if start == "" {
		start = "HEAD"
	}
	head, err := c.resolveCommit(ctx, start)
	if err != nil {
		return "", err
	}
	args := []string{"log", "--no-ext-diff", "--no-textconv", "--no-color", "--no-show-signature", "--date=iso-strict",
		"--format=%H%x09%ad%x09%an%x09%s", "-n", strconv.Itoa(request.LogLimit()), head}
	if request.From != "" {
		from, err := c.resolveCommit(ctx, request.From)
		if err != nil {
			return "", err
		}
		args = append(args, "^"+from)
	}
	return c.readText(ctx, withPath(append(args, "--"), request.Path)...)
}

// resolveCommit returns the hash of the commit rev names, refusing a
// revision the repository holds no commit for.
func (c CLI) resolveCommit(ctx context.Context, rev string) (string, error) {
	out, err := c.readOutput(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if errExitCode(err) == 1 {
		return "", model.GitReadMissing(fmt.Sprintf("revision %q resolves to no commit", rev))
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (c CLI) readText(ctx context.Context, args ...string) (string, error) {
	out, err := c.readOutput(ctx, args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// readOutput runs one read-only git command in the repository, detached from
// any terminal, and returns its standard output; a failure carries git's own
// error output. Output past gitReadMaxBytes stops the command and refuses the
// read, so a large answer never sits in memory whole.
func (c CLI) readOutput(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := CommandAt(ctx, c.Dir, append(slices.Clone(gitReadOptions), args...)...)
	cmd.Env = append(readEnvBase(), gitReadEnv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	err = cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	out, readErr := io.ReadAll(io.LimitReader(stdout, gitReadMaxBytes+1))
	tooLarge := len(out) > gitReadMaxBytes
	if tooLarge {
		cancel()
	}
	waitErr := cmd.Wait()
	if tooLarge {
		return nil, model.GitReadRefusal(fmt.Sprintf("git %s returned more than %d bytes; narrow it with a path or a smaller limit", args[0], gitReadMaxBytes))
	}
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("git %s: timed out after %s", args[0], gitReadTimeout)
	}
	if waitErr != nil {
		return nil, &readError{args: args[0], err: waitErr, stderr: strings.TrimSpace(stderr.String())}
	}
	if readErr != nil {
		return nil, fmt.Errorf("git %s: reading its output: %w", args[0], readErr)
	}
	return out, nil
}

func readEnvBase() []string {
	return slices.DeleteFunc(pathEnv(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return name == "GIT_EXTERNAL_DIFF" || name == "GIT_PAGER" || name == "PAGER"
	})
}

// readError is a failed git read with git's own message, unwrapping to the
// exit error so callers read its code.
type readError struct {
	args   string
	err    error
	stderr string
}

func (e *readError) Error() string {
	if e.stderr == "" {
		return fmt.Sprintf("git %s: %v", e.args, e.err)
	}
	return fmt.Sprintf("git %s: %v: %s", e.args, e.err, e.stderr)
}

func (e *readError) Unwrap() error { return e.err }

func withPath(args []string, path string) []string {
	if path == "" {
		return args
	}
	return append(args, path)
}
