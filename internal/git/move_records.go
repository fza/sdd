package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Head is the commit HEAD names, empty on a branch without commits.
func (c CLI) Head(ctx context.Context) (string, error) {
	born, err := c.hasHead(ctx)
	if err != nil || !born {
		return "", err
	}
	return c.RevParse(ctx, "HEAD")
}

// MoveRecordTrailers are the trailer blocks of the commits in HEAD's history
// carrying a move record, newest first.
// A folder outside any repository has no history and carries none.
func (c CLI) MoveRecordTrailers(ctx context.Context) ([]string, error) {
	if RepoRootFor(c.Dir) == "" {
		return nil, nil
	}
	head, err := c.Head(ctx)
	if err != nil || head == "" {
		return nil, err
	}
	out, err := c.combined(ctx, nil, "", "log", "-E", "--grep=^sdd-move: ", "--format=%(trailers:only,unfold)%x1e", head)
	if err != nil {
		return nil, err
	}
	var blocks []string
	for block := range strings.SplitSeq(out, "\x1e") {
		if strings.TrimSpace(block) != "" {
			blocks = append(blocks, block)
		}
	}
	return blocks, nil
}

// MoveHistory is the finders.MoveHistory over the repository at a directory.
type MoveHistory struct{}

func (MoveHistory) MoveRecordTrailers(ctx context.Context, repoDir string) ([]string, error) {
	return CLI{Dir: repoDir}.MoveRecordTrailers(ctx)
}

func (c CLI) combined(ctx context.Context, env []string, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", c.at(args...)...)
	cmd.Env = env
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
