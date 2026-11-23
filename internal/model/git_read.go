package model

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

// GitReadOp names what a read of the project's git history returns.
type GitReadOp string

// The operations of a git read.
const (
	GitReadFile   GitReadOp = "file"
	GitReadLog    GitReadOp = "log"
	GitReadCommit GitReadOp = "commit"
	GitReadDiff   GitReadOp = "diff"
)

var gitReadOps = []GitReadOp{GitReadFile, GitReadLog, GitReadCommit, GitReadDiff}

// GitReadOps lists the closed set of git read operations.
func GitReadOps() []GitReadOp { return slices.Clone(gitReadOps) }

// GitLogDefaultLimit and GitLogMaxLimit cap the commits a log read lists.
const (
	GitLogDefaultLimit = 20
	GitLogMaxLimit     = 200
)

// ErrGitReadRefused marks a git read refused for its input: an unknown
// operation, a revision or path of a refused form, or one the repository
// does not hold. The caller fixes the request; retrying it unchanged fails
// again.
var ErrGitReadRefused = errors.New("git read refused")

// GitReadRequest is one read of the project's git history. sdd builds the git
// command from it; no field passes through as a git argument of its own.
type GitReadRequest struct {
	Op GitReadOp
	// Rev is the revision a file, log or commit read starts from; a log
	// without one starts from HEAD.
	Rev string
	// From and To are the two revisions a diff compares.
	From, To string
	// Path is a file for a file read, and narrows a log or diff to a path.
	Path string
	// Limit caps a log's commits; zero means GitLogDefaultLimit.
	Limit int
}

// LogLimit is the number of commits a log read lists.
func (r GitReadRequest) LogLimit() int {
	if r.Limit <= 0 {
		return GitLogDefaultLimit
	}
	return min(r.Limit, GitLogMaxLimit)
}

// Validate checks the request's form: a known operation with the revisions
// and path it needs, no revision that git could read as an option, and a
// path inside the checkout. Whether the revisions and the path exist is the
// repository's to say.
func (r GitReadRequest) Validate() error {
	switch r.Op {
	case GitReadFile:
		if r.Rev == "" || r.Path == "" {
			return gitReadRefusal("a file read names rev and path")
		}
	case GitReadLog:
	case GitReadCommit:
		if r.Rev == "" {
			return gitReadRefusal("a commit read names rev")
		}
	case GitReadDiff:
		if r.From == "" || r.To == "" {
			return gitReadRefusal("a diff names from and to")
		}
	default:
		names := make([]string, len(gitReadOps))
		for i, op := range gitReadOps {
			names[i] = string(op)
		}
		return gitReadRefusal(fmt.Sprintf("op %q is not one of %s", r.Op, strings.Join(names, ", ")))
	}
	for _, rev := range []string{r.Rev, r.From, r.To} {
		err := validGitRevision(rev)
		if err != nil {
			return err
		}
	}
	if r.Path != "" && !GitPathInCheckout(r.Path) {
		return gitReadRefusal(fmt.Sprintf("path %q is no path inside the checkout, relative to its root", r.Path))
	}
	return nil
}

// GitPathInCheckout reports whether path names a place inside the checkout,
// relative to its root: no leading slash, no "..", not the root itself.
func GitPathInCheckout(path string) bool {
	return fs.ValidPath(path) && path != "."
}

// ValidGitRevision checks a revision's form: one that git could read as an
// option, or that holds whitespace or a control character, is refused.
func ValidGitRevision(rev string) error {
	if rev == "" {
		return gitReadRefusal("revision is empty")
	}
	return validGitRevision(rev)
}

func validGitRevision(rev string) error {
	if rev == "" {
		return nil
	}
	if strings.HasPrefix(rev, "-") {
		return gitReadRefusal(fmt.Sprintf("revision %q begins with -", rev))
	}
	if strings.ContainsFunc(rev, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return gitReadRefusal(fmt.Sprintf("revision %q holds whitespace or a control character", rev))
	}
	return nil
}

// GitReadRefusal returns a refusal of a git read for its input, matching
// ErrGitReadRefused.
func GitReadRefusal(reason string) error { return gitReadRefusal(reason) }

func gitReadRefusal(reason string) error {
	return fmt.Errorf("%w: %s", ErrGitReadRefused, reason)
}
