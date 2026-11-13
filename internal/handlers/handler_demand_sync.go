package handlers

import (
	"context"
	"fmt"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

// DemandSync executes a DemandSyncCmd: it renews the lease of the graph
// repository under automatic sync and, when the lease starts past the
// effective fetch cooldown, runs one sync round, waited for at most the
// read wait (20261113-232855-d-tac-1nz).
func (h *Handler) DemandSync(ctx context.Context, cmd *command.DemandSyncCmd) error {
	if h.syncMode != model.SyncModeAuto || h.fetcher == nil || !h.fetcher.InRepo(ctx) || !h.fetcher.HasRemote(ctx) {
		return nil
	}
	if h.gate == nil {
		return repos.ErrNoGate
	}
	url, err := h.fetcher.UpstreamURL(ctx)
	if err != nil {
		return fmt.Errorf("resolving the upstream URL: %w", err)
	}
	target, err := gitgate.TargetFromURL(url)
	if err != nil {
		return err
	}
	demand, err := h.gate.Demand(ctx, target)
	if err != nil || !demand.Sync {
		return err
	}
	if cmd.OnRound != nil {
		cmd.OnRound()
	}
	err, finished := waitFor(ctx, h, demand.Wait, func(ctx context.Context) error {
		return h.SyncRound(ctx, &command.SyncRoundCmd{})
	}, func() {
		if cmd.OnPending != nil {
			cmd.OnPending(demand.Fetched)
		}
	}, func(err error) {
		if err != nil {
			slogutils.FromContext(ctx).Warn(fmt.Sprintf("sync: %v", err))
		}
		if cmd.OnSettled != nil {
			cmd.OnSettled()
		}
	})
	if !finished {
		return nil
	}
	return err
}
