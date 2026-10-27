package model

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// GraphCommitSubjectPattern matches the subject every SDD-written commit
// starts with; sync counts commits matching it on either side of the
// local/upstream divergence.
const GraphCommitSubjectPattern = "^sdd:"

// SyncState describes the overall state of background sync detection.
type SyncState string

const (
	// SyncStateUpToDate: no divergence between local and remote.
	SyncStateUpToDate SyncState = "up-to-date"
	// SyncStateFastForward: remote has commits local does not; local has none
	// ahead. A rebase is a fast-forward with no conflict risk.
	SyncStateFastForward SyncState = "fast-forward"
	// SyncStateCleanRebase: both sides have commits, but git merge-tree
	// predicts the rebase would apply cleanly.
	SyncStateCleanRebase SyncState = "clean-rebase"
	// SyncStateConflictPredicted: both sides have diverged and the
	// git merge-tree prediction reports conflicts. Rebase is unsafe without
	// manual resolution.
	SyncStateConflictPredicted SyncState = "conflict-predicted"
	// SyncStateLocalAhead: local has commits remote does not, and remote has
	// none ahead of local. Push is suggested.
	SyncStateLocalAhead SyncState = "local-ahead"
	// SyncStateNoRepo: current directory is not inside a git repository.
	SyncStateNoRepo SyncState = "no-repo"
	// SyncStateNoRemote: repository has no remote configured.
	SyncStateNoRemote SyncState = "no-remote"
	// SyncStateNoUpstream: current branch has no upstream tracking branch.
	SyncStateNoUpstream SyncState = "no-upstream"
	// SyncStateFetchFailed: git fetch was attempted and failed (network,
	// authentication, etc.). The last-fetch timestamp is still updated so
	// subsequent commands within the cooldown window do not retry.
	SyncStateFetchFailed SyncState = "fetch-failed"
	// SyncStateSkipped: the sync check did not run (cooldown window active,
	// command exempt, or environment unavailable). No slog output expected.
	SyncStateSkipped SyncState = "skipped"
)

// Fetched reports whether the sync check reached the upstream, so the
// remote-tracking ref an automatic pull merges is fresh.
func (s SyncState) Fetched() bool {
	switch s {
	case SyncStateUpToDate, SyncStateFastForward, SyncStateCleanRebase, SyncStateConflictPredicted, SyncStateLocalAhead:
		return true
	}
	return false
}

// SyncStatus is the structured result of a background sync check. Callers
// emit the appropriate slog line based on State; the skill pattern-matches
// on the rendered message.
type SyncStatus struct {
	State         SyncState
	RemoteAhead   int      // count of remote-ahead commits matching ^sdd:
	LocalAhead    int      // count of local-ahead commits matching ^sdd:
	ConflictPaths []string // populated when State == SyncStateConflictPredicted
	// Reason carries a short human-readable detail for failure states —
	// the upstream branch name for NoUpstream, the error message for
	// FetchFailed, etc. Empty for success states.
	Reason string
}

// CountMatchingSubjects counts the lines of `git log --format=%s` output,
// one commit subject per line, that pattern matches. Only the subject is
// judged: a body line starting with the graph prefix does not make a commit
// a graph commit.
func CountMatchingSubjects(subjects string, pattern *regexp.Regexp) int {
	n := 0
	for line := range strings.SplitSeq(subjects, "\n") {
		if pattern.MatchString(line) {
			n++
		}
	}
	return n
}

// ParseMergeTreeConflicts parses the output of
// `git merge-tree --write-tree --name-only --no-messages HEAD @{u}`.
// On a clean merge, output is a single tree-OID line → returns nil. On a
// conflict, output is the tree OID followed by conflicted file paths →
// returns the paths. A blank line terminates the path list; any trailing
// informational/conflict-message section (present when --no-messages is
// omitted) is ignored, so the parser stays correct even if the caller
// forgets the flag.
func ParseMergeTreeConflicts(output string) []string {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) <= 1 {
		return nil
	}
	var paths []string
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			break
		}
		paths = append(paths, line)
	}
	return paths
}

// PushVerdict is what automatic sync does with the commits a branch holds
// ahead of its upstream.
type PushVerdict string

const (
	PushOff        PushVerdict = "off"
	PushNoUpstream PushVerdict = "no-upstream"
	PushNothing    PushVerdict = "nothing"
	// PushUnpublished: a commit not written by sdd is ahead, and a push would
	// publish it; the graph commits stay local.
	PushUnpublished PushVerdict = "unpublished"
	PushReady       PushVerdict = "ready"
)

// DecidePush judges a push from the commits ahead of upstream: all of them
// and the ones matching ^sdd:.
func DecidePush(mode SyncMode, hasUpstream bool, ahead, graphAhead int) PushVerdict {
	switch {
	case mode != SyncModeAuto:
		return PushOff
	case !hasUpstream:
		return PushNoUpstream
	case ahead == 0:
		return PushNothing
	case graphAhead < ahead:
		return PushUnpublished
	default:
		return PushReady
	}
}

// PullVerdict is what automatic sync does with the commits upstream holds
// ahead of the branch.
type PullVerdict string

const (
	PullOff        PullVerdict = "off"
	PullNoUpstream PullVerdict = "no-upstream"
	PullNothing    PullVerdict = "nothing"
	// PullForeign: an incoming commit was not written by sdd, so merging it
	// would bring code changes into the working tree unasked.
	PullForeign PullVerdict = "foreign"
	// PullOutsideGraph: an incoming commit changes a path sdd never writes,
	// whatever its subject says (20261027-222018-d-tac-ue8).
	PullOutsideGraph PullVerdict = "outside-graph"
	PullDirty        PullVerdict = "dirty"
	PullReady        PullVerdict = "ready"
)

// IncomingCommits describes the commits upstream holds ahead of the branch.
type IncomingCommits struct {
	All   int
	Graph int
	// Outside lists the changed paths that lie outside the paths sdd writes.
	Outside []string
}

// DecidePull judges a pull from the incoming commits and whether the working
// tree is clean.
func DecidePull(mode SyncMode, hasUpstream bool, incoming IncomingCommits, clean bool) PullVerdict {
	switch {
	case mode != SyncModeAuto:
		return PullOff
	case !hasUpstream:
		return PullNoUpstream
	case incoming.All == 0:
		return PullNothing
	case incoming.Graph < incoming.All:
		return PullForeign
	case len(incoming.Outside) > 0:
		return PullOutsideGraph
	case !clean:
		return PullDirty
	default:
		return PullReady
	}
}

// OutsideGraphPaths returns the paths, relative to the repository holding the
// graph, that lie outside what sdd's own commits write: graphDir (also
// repository-relative; "." makes the whole repository the graph), .sdd/, the
// rendered agent skills and .gitignore.
func OutsideGraphPaths(paths []string, graphDir string) []string {
	graphDir = strings.Trim(filepath.ToSlash(filepath.Clean(graphDir)), "/")
	if graphDir == "." {
		return nil
	}
	prefixes := []string{".sdd/", ".claude/skills/", ".agents/skills/", graphDir + "/"}
	var outside []string
	for _, path := range paths {
		if path == "" || path == ".gitignore" {
			continue
		}
		if !slices.ContainsFunc(prefixes, func(prefix string) bool { return strings.HasPrefix(path, prefix) }) {
			outside = append(outside, path)
		}
	}
	return outside
}

// GraphMergeMessage is the message of the merge commit an automatic pull
// writes; it matches ^sdd: so the merge itself passes the push rule.
func GraphMergeMessage(upstream string) string {
	return fmt.Sprintf("sdd: merge %s", upstream)
}

// MergeConflict is an automatic pull that would not merge cleanly: the
// upstream ref and the paths both sides changed.
type MergeConflict struct {
	Upstream string
	Paths    []string
}

// noticePathLimit caps the paths a conflict notice lists.
const noticePathLimit = 10

// Notice tells the agent how to resolve the conflict by hand, naming the merge
// message that keeps later automatic pushes going (20261027-231225-d-tac-n5o).
// The paths come from upstream commits and reach agent instructions, so each
// is quoted with its control characters escaped and the list is capped.
func (c MergeConflict) Notice() string {
	quoted := make([]string, 0, min(len(c.Paths), noticePathLimit))
	for _, path := range c.Paths[:min(len(c.Paths), noticePathLimit)] {
		quoted = append(quoted, strconv.Quote(path))
	}
	listed := strings.Join(quoted, ", ")
	if more := len(c.Paths) - noticePathLimit; more > 0 {
		listed += fmt.Sprintf(" and %d more", more)
	}
	return fmt.Sprintf("sync: merging %s would conflict in %s. Merge it with git, resolve the conflicts, and commit the merge as %q; automatic sync holds until then.",
		c.Upstream, listed, GraphMergeMessage(c.Upstream))
}
