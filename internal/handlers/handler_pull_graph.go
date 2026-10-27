package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/model"
)

// ErrMergeConflict reports upstream graph commits that would not merge
// cleanly; nothing was merged.
var ErrMergeConflict = errors.New("graph merge would conflict")

// GraphPuller is the git surface automatic sync merges upstream graph
// commits through. Merge leaves the working tree as it found it when the
// merge fails.
type GraphPuller interface {
	UpstreamRef(ctx context.Context) (string, error)
	CountCommits(ctx context.Context, rangeSpec, subjectPattern string) (int, error)
	IsClean(ctx context.Context) (bool, error)
	MergeTreePredict(ctx context.Context, ourRef, theirRef string) ([]string, error)
	Merge(ctx context.Context, ref, message string) error
}

// PullGraph merges the commits upstream holds ahead of the branch when every
// one of them is a graph commit and the working tree is clean, then
// publishes what the branch holds ahead. It reads the remote-tracking ref as
// last fetched; it does not fetch.
func (h *Handler) PullGraph(ctx context.Context, cmd *command.PullGraphCmd) error {
	if h.graphPuller == nil {
		return errors.New("graph pull is unavailable: no git surface configured")
	}
	upstream, err := h.graphPuller.UpstreamRef(ctx)
	if err != nil {
		return fmt.Errorf("resolving upstream: %w", err)
	}
	var incoming, graphIncoming int
	clean := true
	if upstream != "" {
		incoming, err = h.graphPuller.CountCommits(ctx, "HEAD..@{u}", "")
		if err != nil {
			return fmt.Errorf("counting commits behind %s: %w", upstream, err)
		}
		graphIncoming, err = h.graphPuller.CountCommits(ctx, "HEAD..@{u}", model.GraphCommitSubjectPattern)
		if err != nil {
			return fmt.Errorf("counting graph commits behind %s: %w", upstream, err)
		}
		clean, err = h.graphPuller.IsClean(ctx)
		if err != nil {
			return fmt.Errorf("checking working tree: %w", err)
		}
	}

	switch verdict := model.DecidePull(h.syncMode, upstream != "", incoming, graphIncoming, clean); verdict {
	case model.PullReady:
		conflicts, err := h.graphPuller.MergeTreePredict(ctx, "HEAD", upstream)
		if err != nil {
			return fmt.Errorf("predicting merge with %s: %w", upstream, err)
		}
		if len(conflicts) > 0 {
			return fmt.Errorf("%w in %s", ErrMergeConflict, strings.Join(conflicts, ", "))
		}
		err = h.graphPuller.Merge(ctx, upstream, model.GraphMergeMessage(upstream))
		if err != nil {
			return fmt.Errorf("merging %s: %w", upstream, err)
		}
		if cmd.OnMerged != nil {
			cmd.OnMerged(upstream, incoming)
		}
		return h.publishGraph(ctx)
	case model.PullNothing:
		return h.publishGraph(ctx)
	case model.PullForeign, model.PullDirty:
		if cmd.OnHeld != nil {
			cmd.OnHeld(verdict, upstream)
		}
	}
	return nil
}
