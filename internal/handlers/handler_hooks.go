package handlers

import (
	"context"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/hooks"
)

// AllowHooks records the reviewed repositories' hooks as allowed on this
// machine.
func (h *Handler) AllowHooks(ctx context.Context, cmd *command.AllowHooksCmd) error {
	return hooks.New(cmd.Sources, hooks.NewAllows(cmd.StateRoot)).Allow(ctx, cmd.Reviews)
}
