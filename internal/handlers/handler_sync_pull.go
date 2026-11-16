package handlers

import (
	"context"
	"errors"
	"fmt"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/gitgate"
)

// SyncPull pulls the shared graph from upstream using a merge (never a
// rebase), so background sync never rewrites the shared history. It refuses
// on a dirty working tree and defers to the user with an actionable message;
// on a clean tree it runs the merge pull when the git gate admits it and
// reports git's output through the command's OnPulled callback.
func (h *Handler) SyncPull(ctx context.Context, cmd *command.SyncPullCmd) error {
	if h.puller == nil {
		return errors.New("sync pull is unavailable: no git surface configured")
	}

	clean, err := h.puller.IsClean(ctx)
	if err != nil {
		return fmt.Errorf("checking working tree: %w", err)
	}
	if !clean {
		return errors.New("working tree has uncommitted changes — commit or stash before pulling")
	}

	var out string
	err = h.gated(ctx, gitgate.Fetch, h.puller, func(ctx context.Context) error {
		var pullErr error
		out, pullErr = h.puller.MergePull(ctx)
		return pullErr
	})
	deferred, isDeferred := deferral(err)
	if isDeferred {
		if cmd.OnDeferred != nil {
			cmd.OnDeferred(deferred.Next)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("merge pull: %w", err)
	}
	if cmd.OnPulled != nil {
		cmd.OnPulled(out)
	}
	return nil
}
