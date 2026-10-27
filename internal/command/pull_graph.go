package command

import "github.com/networkteam/sdd/internal/model"

// PullGraphCmd captures intent to merge the graph commits upstream holds
// ahead of the branch, under automatic sync (20261027-194536-d-tac-190).
type PullGraphCmd struct {
	// OnMerged is invoked after a merge with the upstream and the number of
	// commits it brought in.
	OnMerged func(upstream string, commits int)
	// OnHeld is invoked when the upstream commits are left unmerged: one of
	// them was not written by sdd, changes a path sdd never writes (listed in
	// outside), or the working tree is dirty.
	OnHeld func(verdict model.PullVerdict, upstream string, outside []string)
}
