package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/model"
)

// Pusher is the git surface automatic sync publishes graph commits through.
type Pusher interface {
	// UpstreamRef returns the current branch's upstream, empty when it has
	// none.
	UpstreamRef(ctx context.Context) (string, error)
	// CountCommits counts commits in rangeSpec whose subject matches
	// subjectPattern; an empty pattern counts every commit.
	CountCommits(ctx context.Context, rangeSpec, subjectPattern string) (int, error)
	Push(ctx context.Context) error
}

// PushGraph publishes the branch's commits ahead of its upstream when every
// one of them is a graph commit, and holds them otherwise.
func (h *Handler) PushGraph(ctx context.Context, cmd *command.PushGraphCmd) error {
	if h.pusher == nil {
		return errors.New("graph push is unavailable: no git surface configured")
	}
	upstream, err := h.pusher.UpstreamRef(ctx)
	if err != nil {
		return fmt.Errorf("resolving upstream: %w", err)
	}
	var ahead, graphAhead int
	if upstream != "" {
		ahead, err = h.pusher.CountCommits(ctx, "@{u}..HEAD", "")
		if err != nil {
			return fmt.Errorf("counting commits ahead of %s: %w", upstream, err)
		}
		graphAhead, err = h.pusher.CountCommits(ctx, "@{u}..HEAD", model.GraphCommitSubjectPattern)
		if err != nil {
			return fmt.Errorf("counting graph commits ahead of %s: %w", upstream, err)
		}
	}

	switch verdict := model.DecidePush(h.syncMode, upstream != "", ahead, graphAhead); verdict {
	case model.PushReady:
		err = h.pusher.Push(ctx)
		if err != nil {
			return fmt.Errorf("pushing to %s: %w", upstream, err)
		}
		if cmd.OnPushed != nil {
			cmd.OnPushed(upstream, ahead)
		}
	case model.PushNoUpstream, model.PushUnpublished:
		if cmd.OnHeld != nil {
			cmd.OnHeld(verdict, upstream)
		}
	}
	return nil
}

// commitGraph commits paths and, under automatic sync, publishes the commit
// before the command returns. A failed push fails the command; the commit
// stays.
func (h *Handler) commitGraph(ctx context.Context, message string, paths ...string) error {
	err := h.committer.Commit(message, paths...)
	if err != nil {
		return err
	}
	return h.publishGraph(ctx)
}

// publishGraph pushes the graph commits ahead of upstream under automatic
// sync and reports the ones it holds back. A push the upstream rejects is
// retried once after merging what upstream gained, by the pull rules
// (20261027-221041-d-tac-8b7).
func (h *Handler) publishGraph(ctx context.Context) error {
	if h.syncMode != model.SyncModeAuto {
		return nil
	}
	err := h.PushGraph(ctx, h.reportingPush(ctx))
	if err == nil {
		return nil
	}
	pushErr := fmt.Errorf("publishing graph commit: %w", err)
	if h.graphPuller == nil {
		return pushErr
	}
	err = h.graphPuller.Fetch(ctx)
	if err != nil {
		return errors.Join(pushErr, fmt.Errorf("fetching to retry: %w", err))
	}
	verdict, err := h.mergeUpstream(ctx, h.reportingPull(ctx))
	if err != nil {
		return errors.Join(pushErr, err)
	}
	if verdict != model.PullReady {
		return pushErr
	}
	err = h.PushGraph(ctx, h.reportingPush(ctx))
	if err != nil {
		return fmt.Errorf("publishing graph commit after merging upstream: %w", err)
	}
	return nil
}

func (h *Handler) reportingPush(ctx context.Context) *command.PushGraphCmd {
	log := slogutils.FromContext(ctx)
	return &command.PushGraphCmd{
		OnPushed: func(upstream string, commits int) {
			log.Info(fmt.Sprintf("sync: pushed %d commits to %s", commits, upstream))
		},
		OnHeld: func(verdict model.PushVerdict, upstream string) {
			if verdict == model.PushNoUpstream {
				log.Warn("sync: captured but unpublished: the branch has no upstream; set one with `git branch --set-upstream-to=origin/<branch>`")
				return
			}
			log.Warn(fmt.Sprintf("sync: captured but unpublished: a commit not written by sdd is ahead of %s, so nothing was pushed", upstream))
		},
	}
}

func (h *Handler) reportingPull(ctx context.Context) *command.PullGraphCmd {
	log := slogutils.FromContext(ctx)
	return &command.PullGraphCmd{
		OnMerged: func(upstream string, commits int) {
			log.Info(fmt.Sprintf("sync: merged %d commits from %s", commits, upstream))
		},
		OnHeld: func(verdict model.PullVerdict, upstream string, outside []string) {
			switch verdict {
			case model.PullDirty:
				log.Warn(fmt.Sprintf("sync: not pulling %s: the working tree has uncommitted changes", upstream))
			case model.PullOutsideGraph:
				log.Warn(fmt.Sprintf("sync: not pulling %s: a commit there changes %s, outside the graph; pull it yourself", upstream, strings.Join(outside, ", ")))
			default:
				log.Warn(fmt.Sprintf("sync: not pulling %s: a commit there was not written by sdd; pull it yourself", upstream))
			}
		},
	}
}
