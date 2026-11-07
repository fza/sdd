package handlers

import (
	"context"
	"errors"
	"fmt"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/command"
)

// GraphCloneKeeper restores the sdd-owned graph clone to what only sdd made
// before an operation on it (20261106-202055-d-tac-eyl).
type GraphCloneKeeper interface {
	KeepGraphClone(ctx context.Context, routing []string) error
}

func (h *Handler) keepGraphClone(ctx context.Context) error {
	if h.graphClone == nil {
		return nil
	}
	err := h.graphClone.KeepGraphClone(ctx, nil)
	if err != nil {
		return fmt.Errorf("keeping the graph clone: %w", err)
	}
	return nil
}

// EnsureGraphClone clones the graph repository when this machine holds no
// clone of it yet, and keeps the clone otherwise.
func (h *Handler) EnsureGraphClone(ctx context.Context, cmd *command.EnsureGraphCloneCmd) error {
	if h.graphClones == nil {
		return errors.New("graph clones are unavailable: no clone location configured")
	}
	dir, cloned, err := h.graphClones.Ensure(ctx, cmd.RepoID, cmd.CodeOrigin)
	if err != nil {
		return err
	}
	if cloned {
		slogutils.FromContext(ctx).Info(fmt.Sprintf("cloned graph repository %s into %s", cmd.RepoID, dir))
		if cmd.OnCloned != nil {
			cmd.OnCloned(dir)
		}
	}
	return nil
}
