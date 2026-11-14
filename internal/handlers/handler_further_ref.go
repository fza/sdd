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

// ResolveFurtherRef executes a ResolveFurtherRefCmd. A repo that is not
// connected, a recorded miss whose clone has not moved within a day, a fetch
// the gate defers or that outlasts read_wait, and a completed fetch still
// lacking the entry each end in cmd.OnUnavailable; the last records the miss
// beside the gate's record. Only a failure to read or record that state is
// returned.
func (h *Handler) ResolveFurtherRef(ctx context.Context, cmd *command.ResolveFurtherRefCmd) error {
	err := cmd.Validate()
	if err != nil {
		return fmt.Errorf("invalid command: %w", err)
	}
	if h.repos == nil {
		return errNoRepos
	}
	if h.cloneRound == nil {
		return errNoCloneRound
	}
	unavailable := func(kind model.UnavailableRefKind, at model.UnavailableRef) {
		at.Repo, at.EntryID, at.Kind = cmd.RepoID, cmd.EntryID, kind
		cmd.OnUnavailable(at)
	}
	cfg, err := h.repos.Registry().Load()
	if err != nil {
		return err
	}
	repo, connected := cfg.Connected(cmd.RepoID)
	if !connected {
		unavailable(model.RefNotConnected, model.UnavailableRef{At: h.now()})
		return nil
	}
	dir, err := h.repos.Registry().CloneDir(cmd.RepoID)
	if err != nil {
		return err
	}
	clone := repos.Clone{RepoID: cmd.RepoID, Dir: dir}
	if clone.Cloned() {
		holds, err := clone.Holds(cmd.EntryID)
		if err != nil || holds {
			return err
		}
		if cmd.Fetched {
			return h.recordRefMiss(ctx, clone, cmd)
		}
		commit, err := clone.Head()
		if err != nil {
			return err
		}
		miss, found, err := h.repos.RefMisses().Find(ctx, cmd.RepoID, cmd.EntryID)
		if err != nil {
			return err
		}
		if found && !miss.Retry(commit, h.now()) {
			unavailable(model.RefMissingAfterFetch, model.UnavailableRef{At: miss.At})
			return nil
		}
	}

	wait, err := h.repos.ReadWait(ctx, repo)
	if err != nil {
		return err
	}
	fetchCtx := gitgate.WithCredits(ctx, gitgate.Fetch)
	outcome, finished := waitFor(fetchCtx, h, wait, func(ctx context.Context) model.RepoSyncOutcome {
		outcome, _ := h.syncConnected(ctx, repo, connectedSync{clone: true})
		return outcome
	}, func() {}, func(outcome model.RepoSyncOutcome) {
		if outcome.State == model.RepoFailed {
			slogutils.FromContext(ctx).Warn("fetch on a miss failed", "repo", outcome.RepoID, "reason", outcome.Reason)
		}
	})
	switch {
	case !finished:
		unavailable(model.RefFetchDeferred, model.UnavailableRef{At: h.now()})
		return nil
	case outcome.State == model.RepoSkipped || outcome.State == model.RepoFailed:
		if outcome.State == model.RepoFailed {
			slogutils.FromContext(ctx).Warn("fetch on a miss failed", "repo", outcome.RepoID, "reason", outcome.Reason)
		}
		unavailable(model.RefFetchDeferred, model.UnavailableRef{At: outcome.Next})
		return nil
	}
	if cmd.OnFetched != nil {
		cmd.OnFetched()
	}
	holds, err := clone.Holds(cmd.EntryID)
	if err != nil || holds {
		return err
	}
	return h.recordRefMiss(ctx, clone, cmd)
}

// recordRefMiss records that clone, after a completed fetch, lacks the entry.
func (h *Handler) recordRefMiss(ctx context.Context, clone repos.Clone, cmd *command.ResolveFurtherRefCmd) error {
	commit, err := clone.Head()
	if err != nil {
		return err
	}
	miss := model.RefMiss{Repo: cmd.RepoID, EntryID: cmd.EntryID, Commit: commit, At: h.now().UTC()}
	err = h.repos.RefMisses().Record(ctx, miss)
	if err != nil {
		return err
	}
	cmd.OnUnavailable(model.UnavailableRef{Repo: cmd.RepoID, EntryID: cmd.EntryID, Kind: model.RefMissingAfterFetch, At: miss.At})
	return nil
}
