package handlers

import (
	"context"
	"fmt"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// GraphLock serializes every writer of one graph directory across processes.
type GraphLock interface {
	// LockGraph waits for the graph lock until ctx ends.
	LockGraph(ctx context.Context) (release func(), err error)
}

// TitlesLock keeps one title backfill running per checkout.
type TitlesLock interface {
	// TryLockTitles takes the lock when it is free; release is nil when a
	// backfill holds it.
	TryLockTitles(ctx context.Context) (release func(), err error)
}

// BackfillTitles titles the graph's untitled entries through the batch path
// of Summarize and commits them as one `sdd: summarize N entries` commit
// (20261105-233807-d-tac-831). A failure is recorded beside the blocked-sync
// record for the sync notice, never failing the trigger; a run that succeeds
// clears it.
func (h *Handler) BackfillTitles(ctx context.Context, cmd *command.BackfillTitlesCmd) error {
	skip := func() {
		if cmd.OnSkipped != nil {
			cmd.OnSkipped()
		}
	}
	if h.titlesLock != nil {
		release, err := h.titlesLock.TryLockTitles(ctx)
		if err != nil {
			return fmt.Errorf("taking the title backfill lock: %w", err)
		}
		if release == nil {
			skip()
			return nil
		}
		defer release()
	}
	if cmd.SDDDir != "" {
		last, err := meta.ReadLastTitles(cmd.SDDDir)
		if err != nil {
			return fmt.Errorf("reading last-titles: %w", err)
		}
		if cmd.Cooldown > 0 && !last.IsZero() && h.now().Sub(last) < cmd.Cooldown {
			skip()
			return nil
		}
		// Stamped whatever the outcome: the cooldown bounds attempts, so a
		// failing LLM is not retried by every call.
		err = meta.TouchLastTitles(cmd.SDDDir)
		if err != nil {
			return fmt.Errorf("stamping last-titles: %w", err)
		}
	}
	if h.llmRunner == nil {
		return h.recordTitlesFailed(ctx, "no LLM is configured")
	}
	err := h.Summarize(ctx, &command.SummarizeCmd{Backfill: true})
	if err != nil {
		slogutils.FromContext(ctx).Warn("title backfill failed", "err", err)
		return h.recordTitlesFailed(ctx, "a title call or its commit failed")
	}
	return h.clearRecord(model.SyncBlocked.TitlesFailed)
}

// recordTitlesFailed records a failed backfill unless a blocked sync is
// recorded, which takes precedence, and reports it either way. The detail is
// a fixed phrase: the notice is served to agents as instructions, so an
// error's own text is never passed on.
func (h *Handler) recordTitlesFailed(ctx context.Context, detail string) error {
	block := model.SyncBlocked{Kind: model.SyncBlockedTitles, Since: h.now(), Repo: h.graphDir, Detail: detail}
	if h.syncRecord == nil {
		h.reportBlocked(ctx, block)
		return nil
	}
	renewed := block
	err := h.syncRecord.Update(func(current *model.SyncBlocked) *model.SyncBlocked {
		if current != nil && !current.TitlesFailed() {
			return current
		}
		renewed = block.Renew(current)
		return &renewed
	})
	if err != nil {
		return fmt.Errorf("recording the failed title backfill: %w", err)
	}
	h.reportBlocked(ctx, renewed)
	return nil
}
