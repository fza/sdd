package handlers

import (
	"context"
	"errors"
	"fmt"

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
// sync and reports the ones it holds back.
func (h *Handler) publishGraph(ctx context.Context) error {
	if h.syncMode != model.SyncModeAuto {
		return nil
	}
	log := slogutils.FromContext(ctx)
	err := h.PushGraph(ctx, &command.PushGraphCmd{
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
	})
	if err != nil {
		return fmt.Errorf("publishing graph commit: %w", err)
	}
	return nil
}
