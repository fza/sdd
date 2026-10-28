package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SyncBlockedKind names why automatic sync of the graph repository cannot
// proceed (20261028-201909-d-tac-fxp).
type SyncBlockedKind string

const (
	SyncBlockedConflict     SyncBlockedKind = "conflict"
	SyncBlockedForeign      SyncBlockedKind = "foreign-incoming"
	SyncBlockedOutsideGraph SyncBlockedKind = "outside-graph-incoming"
	SyncBlockedDirty        SyncBlockedKind = "dirty"
	SyncBlockedUnpublished  SyncBlockedKind = "unpublished"
	SyncBlockedNoUpstream   SyncBlockedKind = "no-upstream"
	SyncBlockedFetchFailed  SyncBlockedKind = "fetch-failed"
	SyncBlockedPushFailed   SyncBlockedKind = "push-failed"
	SyncBlockedGraphMissing SyncBlockedKind = "graph-missing"
)

// SyncBlocked records a blocked automatic sync: what blocks it, since when,
// and the session of the agent that owns the fix. A conflict also records the
// upstream commit that would not merge (Source) and the local commit it was to
// merge into (Destination).
type SyncBlocked struct {
	Kind        SyncBlockedKind `json:"kind"`
	Since       time.Time       `json:"since"`
	Repo        string          `json:"repo,omitempty"`
	Upstream    string          `json:"upstream,omitempty"`
	Detail      string          `json:"detail,omitempty"`
	Source      string          `json:"source,omitempty"`
	Destination string          `json:"destination,omitempty"`
	Paths       []string        `json:"paths,omitempty"`
	Owner       string          `json:"owner,omitempty"`
}

// HeldPullBlock is the blocked state a held pull verdict stands for, and
// false for a verdict that blocks nothing.
func HeldPullBlock(verdict PullVerdict) (SyncBlockedKind, bool) {
	switch verdict {
	case PullForeign:
		return SyncBlockedForeign, true
	case PullOutsideGraph:
		return SyncBlockedOutsideGraph, true
	case PullDirty:
		return SyncBlockedDirty, true
	case PullNoUpstream:
		return SyncBlockedNoUpstream, true
	}
	return "", false
}

// HeldPushBlock is the blocked state a held push verdict stands for, and
// false for a verdict that blocks nothing.
func HeldPushBlock(verdict PushVerdict) (SyncBlockedKind, bool) {
	switch verdict {
	case PushUnpublished:
		return SyncBlockedUnpublished, true
	case PushNoUpstream:
		return SyncBlockedNoUpstream, true
	}
	return "", false
}

// Pauses reports whether the block stops every automatic fetch, merge and
// push until it is resolved by hand: only a conflict does, since fetching
// again raises the same conflict. Other blocks are retried each round.
func (b SyncBlocked) Pauses() bool { return b.Kind == SyncBlockedConflict }

// ClearedByPush reports whether a successful push resolves the block.
func (b SyncBlocked) ClearedByPush() bool {
	switch b.Kind {
	case SyncBlockedUnpublished, SyncBlockedNoUpstream, SyncBlockedPushFailed:
		return true
	}
	return false
}

// Persisted reports whether the block can be recorded: a missing graph
// repository has no git directory to record it in.
func (b SyncBlocked) Persisted() bool { return b.Kind != SyncBlockedGraphMissing }

// Renew returns b carrying over the start time and owner of the block it
// replaces when both are the same kind, so a block found again each round
// keeps the time it began and the agent fixing it.
func (b SyncBlocked) Renew(previous *SyncBlocked) SyncBlocked {
	if previous != nil && previous.Kind == b.Kind {
		b.Since = previous.Since
		b.Owner = previous.Owner
	}
	return b
}

// Claim returns b owned by session when no agent owns it yet.
func (b SyncBlocked) Claim(session string) SyncBlocked {
	if b.Owner == "" {
		b.Owner = session
	}
	return b
}

// Banner is the one line every response carries while the block lasts.
func (b SyncBlocked) Banner() string {
	return fmt.Sprintf("sync blocked since %s: %s", b.Since.Local().Format("2006-01-02 15:04"), b.reason())
}

// NoticeFor is what an agent's response carries: the banner, plus the fix
// steps for the owning session; every other session is told to leave the fix
// to the owner.
func (b SyncBlocked) NoticeFor(session string) string {
	if b.Owner != "" && b.Owner == session {
		return b.Banner() + "\n" + b.Steps()
	}
	return b.Banner() + ". Another agent owns the fix; leave it to that agent."
}

func (b SyncBlocked) reason() string {
	switch b.Kind {
	case SyncBlockedConflict:
		return fmt.Sprintf("merging %s would conflict in %s; automatic sync is paused", b.Upstream, quotedPaths(b.Paths))
	case SyncBlockedForeign:
		return fmt.Sprintf("%s holds a commit not written by sdd, so nothing is pulled", b.Upstream)
	case SyncBlockedOutsideGraph:
		return fmt.Sprintf("%s changes %s, outside the graph, so nothing is pulled", b.Upstream, quotedPaths(b.Paths))
	case SyncBlockedDirty:
		return "the graph repository has uncommitted changes, so nothing is pulled"
	case SyncBlockedUnpublished:
		return fmt.Sprintf("a commit not written by sdd is ahead of %s, so graph commits stay unpublished", b.Upstream)
	case SyncBlockedNoUpstream:
		return "the branch has no upstream, so graph commits stay unpublished"
	case SyncBlockedFetchFailed:
		return fmt.Sprintf("fetching failed: %s", b.Detail)
	case SyncBlockedPushFailed:
		return fmt.Sprintf("pushing to %s failed: %s", b.Upstream, b.Detail)
	case SyncBlockedGraphMissing:
		return fmt.Sprintf("the graph directory %s is not in a git repository or does not exist", strconv.Quote(b.Detail))
	}
	return string(b.Kind)
}

// Steps tells the owning agent how to clear the block, fixing it itself
// first and asking the user only when it cannot.
func (b SyncBlocked) Steps() string {
	where := "the graph repository"
	if b.Repo != "" {
		where = fmt.Sprintf("the graph repository %s", b.Repo)
	}
	var fix string
	switch b.Kind {
	case SyncBlockedConflict:
		fix = fmt.Sprintf("Run `git merge %s`, resolve the conflicts, and commit the merge as %q; sync resumes once %s is in the branch.",
			b.Upstream, GraphMergeMessage(b.Upstream), shortCommit(b.Source))
	case SyncBlockedForeign, SyncBlockedOutsideGraph:
		fix = fmt.Sprintf("Review the incoming commits with `git log HEAD..%s`; sdd never merges them unreviewed. Ask the user before merging them.", b.Upstream)
	case SyncBlockedDirty:
		fix = "Commit or stash what `git status` lists."
	case SyncBlockedUnpublished:
		fix = fmt.Sprintf("See `git log %s..HEAD`; sdd never pushes a commit it did not write. Ask the user whether to push or move it.", b.Upstream)
	case SyncBlockedNoUpstream:
		fix = "Set an upstream with `git branch --set-upstream-to=origin/<branch>`, or ask the user which remote branch to use."
	case SyncBlockedFetchFailed, SyncBlockedPushFailed:
		fix = "Check the remote and its authentication with `git fetch`; a returning network clears it on the next round."
	case SyncBlockedGraphMissing:
		fix = "Restore the graph directory or correct graph_dir in the config."
	}
	return fmt.Sprintf("You own this block, in %s. %s Fix it yourself first and tell the user in one line once it is fixed. "+
		"When you cannot, such as for commits you may not rewrite, an authentication failure or a missing repository, tell the user and ask how to proceed.",
		where, fix)
}

// gitFailures maps messages git itself prints on a failed fetch or push to
// fixed descriptions, most specific first.
var gitFailures = []struct{ marker, description string }{
	{"Could not resolve host", "the remote host could not be resolved"},
	{"Authentication failed", "authentication with the remote failed"},
	{"Permission denied", "authentication with the remote failed"},
	{"could not read Username", "authentication with the remote failed"},
	{"[remote rejected]", "the remote declined the push"},
	{"non-fast-forward", "the remote holds commits not merged here"},
	{"fetch first", "the remote holds commits not merged here"},
	{"[rejected]", "the remote rejected the push"},
	{"Repository not found", "the remote repository does not exist"},
	{"does not appear to be a git repository", "the remote repository does not exist"},
	{"Could not read from remote repository", "the remote repository could not be read"},
	{"unable to access", "the remote could not be reached"},
	{"Connection refused", "the remote could not be reached"},
	{"timed out", "the remote could not be reached"},
	{"hung up", "the remote closed the connection"},
}

// GitFailureDetail describes a failed fetch or push by a fixed phrase picked
// from git's own messages. Git's output is never passed on: the remote server
// writes part of it, and the notice is served to agents as instructions.
func GitFailureDetail(err error) string {
	message := err.Error()
	for _, failure := range gitFailures {
		if strings.Contains(message, failure.marker) {
			return failure.description
		}
	}
	return "for a reason git did not name in a known message; run the command by hand to see it"
}

// noticePathLimit caps the paths a notice lists.
const noticePathLimit = 10

// quotedPaths lists paths from upstream commits for agent instructions, each
// quoted with its control characters escaped, the list capped.
func quotedPaths(paths []string) string {
	quoted := make([]string, 0, min(len(paths), noticePathLimit))
	for _, path := range paths[:min(len(paths), noticePathLimit)] {
		quoted = append(quoted, strconv.Quote(path))
	}
	listed := strings.Join(quoted, ", ")
	if more := len(paths) - noticePathLimit; more > 0 {
		listed += fmt.Sprintf(" and %d more", more)
	}
	return listed
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
