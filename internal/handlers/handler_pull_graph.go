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

// MergeConflictError carries the conflict an automatic pull stopped at; it
// matches ErrMergeConflict.
type MergeConflictError struct {
	model.MergeConflict
}

func (e *MergeConflictError) Error() string {
	return fmt.Sprintf("%v in %s", ErrMergeConflict, strings.Join(e.Paths, ", "))
}

func (e *MergeConflictError) Unwrap() error { return ErrMergeConflict }

// GraphPuller is the git surface automatic sync merges upstream graph
// commits through. Merge leaves the working tree as it found it when the
// merge fails.
type GraphPuller interface {
	UpstreamRef(ctx context.Context) (string, error)
	Fetch(ctx context.Context) error
	CountCommits(ctx context.Context, rangeSpec, subjectPattern string) (int, error)
	ChangedPaths(ctx context.Context, base, tip string) ([]string, error)
	RepoPath(ctx context.Context, path string) (string, error)
	IsClean(ctx context.Context) (bool, error)
	MergeTreePredict(ctx context.Context, ourRef, theirRef string) ([]string, error)
	Merge(ctx context.Context, ref, message string) error
}

// PullGraph merges the commits upstream holds ahead of the branch when they
// pass the pull rules, then publishes what the branch holds ahead. It reads
// the remote-tracking ref as last fetched; it does not fetch.
func (h *Handler) PullGraph(ctx context.Context, cmd *command.PullGraphCmd) error {
	verdict, err := h.mergeUpstream(ctx, cmd)
	if err != nil {
		return err
	}
	if verdict == model.PullReady || verdict == model.PullNothing {
		return h.publishGraph(ctx)
	}
	return nil
}

// PublishGraph pushes the graph commits ahead of upstream under automatic
// sync, retrying a push the upstream rejected once after merging.
func (h *Handler) PublishGraph(ctx context.Context) error {
	return h.publishGraph(ctx)
}

// AutoPullGraph runs PullGraph reporting through the context's logger, the
// way every automatic sync step reports.
func (h *Handler) AutoPullGraph(ctx context.Context) error {
	return h.PullGraph(ctx, h.reportingPull(ctx))
}

// mergeUpstream merges the incoming commits when DecidePull finds them ready
// and reports held ones through cmd. It returns the verdict it acted on.
func (h *Handler) mergeUpstream(ctx context.Context, cmd *command.PullGraphCmd) (model.PullVerdict, error) {
	if h.graphPuller == nil {
		return "", errors.New("graph pull is unavailable: no git surface configured")
	}
	upstream, err := h.graphPuller.UpstreamRef(ctx)
	if err != nil {
		return "", fmt.Errorf("resolving upstream: %w", err)
	}
	var incoming model.IncomingCommits
	clean := true
	if upstream != "" {
		incoming, err = h.incomingCommits(ctx, upstream)
		if err != nil {
			return "", err
		}
		clean, err = h.graphPuller.IsClean(ctx)
		if err != nil {
			return "", fmt.Errorf("checking working tree: %w", err)
		}
	}

	verdict := model.DecidePull(h.syncMode, upstream != "", incoming, clean)
	switch verdict {
	case model.PullReady:
		conflicts, err := h.graphPuller.MergeTreePredict(ctx, "HEAD", upstream)
		if err != nil {
			return "", fmt.Errorf("predicting merge with %s: %w", upstream, err)
		}
		if len(conflicts) > 0 {
			return "", &MergeConflictError{model.MergeConflict{Upstream: upstream, Paths: conflicts}}
		}
		err = h.graphPuller.Merge(ctx, upstream, model.GraphMergeMessage(upstream))
		if err != nil {
			return "", fmt.Errorf("merging %s: %w", upstream, err)
		}
		if cmd.OnMerged != nil {
			cmd.OnMerged(upstream, incoming.All)
		}
	case model.PullForeign, model.PullOutsideGraph, model.PullDirty:
		if cmd.OnHeld != nil {
			cmd.OnHeld(verdict, upstream, incoming.Outside)
		}
	}
	return verdict, nil
}

func (h *Handler) incomingCommits(ctx context.Context, upstream string) (model.IncomingCommits, error) {
	var incoming model.IncomingCommits
	var err error
	incoming.All, err = h.graphPuller.CountCommits(ctx, "HEAD..@{u}", "")
	if err != nil {
		return incoming, fmt.Errorf("counting commits behind %s: %w", upstream, err)
	}
	incoming.Graph, err = h.graphPuller.CountCommits(ctx, "HEAD..@{u}", model.GraphCommitSubjectPattern)
	if err != nil {
		return incoming, fmt.Errorf("counting graph commits behind %s: %w", upstream, err)
	}
	if incoming.All == 0 {
		return incoming, nil
	}
	paths, err := h.graphPuller.ChangedPaths(ctx, "HEAD", "@{u}")
	if err != nil {
		return incoming, fmt.Errorf("listing paths changed on %s: %w", upstream, err)
	}
	graphDir := model.DefaultGraphDir
	if h.graphDir != "" {
		graphDir, err = h.graphPuller.RepoPath(ctx, h.graphDir)
		if err != nil {
			return incoming, fmt.Errorf("locating the graph directory: %w", err)
		}
	}
	incoming.Outside = model.OutsideGraphPaths(paths, graphDir)
	return incoming, nil
}
