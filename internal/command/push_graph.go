package command

import "github.com/networkteam/sdd/internal/model"

// PushGraphCmd captures intent to publish the graph commits the branch holds
// ahead of its upstream, under automatic sync.
type PushGraphCmd struct {
	// OnPushed is invoked after a push with the upstream and the number of
	// commits it received.
	OnPushed func(upstream string, commits int)
	// OnHeld is invoked when the commits stay local: the branch has no
	// upstream, or a commit not written by sdd is ahead of it.
	OnHeld func(verdict model.PushVerdict, upstream string)
}
