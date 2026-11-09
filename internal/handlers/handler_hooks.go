package handlers

import (
	"context"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/hooks"
)

// ConfirmHook records a hook's hash as confirmed on this machine.
func (h *Handler) ConfirmHook(_ context.Context, cmd *command.ConfirmHookCmd) error {
	return hooks.NewStore(cmd.StateRoot).Confirm(cmd.Hook)
}
