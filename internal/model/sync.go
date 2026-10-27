package model

import (
	"fmt"
	"strings"
)

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

// CountGraphCommits counts non-empty lines from `git log --grep='^sdd:'
// --pretty=format:%H <range>`. Each line is one SDD-written commit in the
// range; we only need the count, not the hashes themselves.
func CountGraphCommits(gitLogOutput string) int {
	n := 0
	for line := range strings.SplitSeq(gitLogOutput, "\n") {
		if strings.TrimSpace(line) != "" {
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
	PullDirty   PullVerdict = "dirty"
	PullReady   PullVerdict = "ready"
)

// DecidePull judges a pull from the commits upstream holds ahead, all of
// them and the ones matching ^sdd:, and whether the working tree is clean.
func DecidePull(mode SyncMode, hasUpstream bool, incoming, graphIncoming int, clean bool) PullVerdict {
	switch {
	case mode != SyncModeAuto:
		return PullOff
	case !hasUpstream:
		return PullNoUpstream
	case incoming == 0:
		return PullNothing
	case graphIncoming < incoming:
		return PullForeign
	case !clean:
		return PullDirty
	default:
		return PullReady
	}
}

// GraphMergeMessage is the message of the merge commit an automatic pull
// writes; it matches ^sdd: so the merge itself passes the push rule.
func GraphMergeMessage(upstream string) string {
	return fmt.Sprintf("sdd: merge %s", upstream)
}

// MergeConflict is one path an automatic pull could not merge: the content
// at the merge base and on each side. Base is empty when both sides added
// the path.
type MergeConflict struct {
	Path   string
	Base   string
	Ours   string
	Theirs string
}
