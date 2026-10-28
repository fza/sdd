package handlers

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// SyncLock serializes every sync path on one graph repository across
// processes (20261028-175735-d-tac-k4y).
type SyncLock interface {
	// TryLock takes the lock when it is free; release is nil when another
	// holder has it.
	TryLock(ctx context.Context) (release func(), err error)
	// Lock waits for the lock until ctx ends.
	Lock(ctx context.Context) (release func(), err error)
}

// SyncRecord holds the blocked-sync record of one graph repository.
type SyncRecord interface {
	Load() (*model.SyncBlocked, error)
	// Update replaces the record with what change returns for the current
	// one, atomically across processes; nil removes it.
	Update(change func(current *model.SyncBlocked) *model.SyncBlocked) error
}

// GraphFetcher is the git surface a sync round fetches the upstream through.
type GraphFetcher interface {
	InRepo(ctx context.Context) bool
	HasRemote(ctx context.Context) bool
	UpstreamRef(ctx context.Context) (string, error)
	Fetch(ctx context.Context) error
}

// FetchGraph fetches the graph repository's upstream unless the last fetch
// lies within the cooldown, and stamps the last-fetch marker.
func (h *Handler) FetchGraph(ctx context.Context, cmd *command.FetchGraphCmd) error {
	if h.fetcher == nil {
		return errors.New("graph fetch is unavailable: no git surface configured")
	}
	report := func(state model.SyncState, reason string) {
		if cmd.OnFetched != nil {
			cmd.OnFetched(state, reason)
		}
	}
	if !h.fetcher.InRepo(ctx) {
		report(model.SyncStateNoRepo, "")
		return nil
	}
	if cmd.Cooldown > 0 {
		last, err := meta.ReadLastFetch(cmd.SDDDir)
		if err != nil {
			return fmt.Errorf("reading last-fetch: %w", err)
		}
		if !last.IsZero() && h.now().Sub(last) < cmd.Cooldown {
			report(model.SyncStateSkipped, "")
			return nil
		}
	}
	state, reason, err := h.fetchUpstream(ctx)
	if err != nil {
		return err
	}
	// Stamped whatever the outcome: the cooldown bounds attempts, so an
	// offline remote is not retried by every command.
	err = meta.TouchLastFetch(cmd.SDDDir)
	if err != nil {
		return fmt.Errorf("stamping last-fetch: %w", err)
	}
	report(state, reason)
	return nil
}

func (h *Handler) fetchUpstream(ctx context.Context) (model.SyncState, string, error) {
	if !h.fetcher.HasRemote(ctx) {
		return model.SyncStateNoRemote, "", nil
	}
	upstream, err := h.fetcher.UpstreamRef(ctx)
	if err != nil {
		return "", "", fmt.Errorf("resolving upstream: %w", err)
	}
	if upstream == "" {
		return model.SyncStateNoUpstream, "", nil
	}
	err = h.fetcher.Fetch(ctx)
	if err != nil {
		return model.SyncStateFetchFailed, err.Error(), nil
	}
	return model.SyncStateFetched, "", nil
}

// SyncRound runs one automatic sync round under the sync lock: fetch, merge
// upstream graph commits, publish local ones, and record what blocks them,
// removing the record once nothing does. While a conflict is recorded only an
// explicit round fetches, merges or pushes (20261028-201909-d-tac-fxp).
func (h *Handler) SyncRound(ctx context.Context, cmd *command.SyncRoundCmd) error {
	if h.syncMode != model.SyncModeAuto {
		return nil
	}
	if h.fetcher == nil {
		h.reportBlocked(ctx, model.SyncBlocked{Kind: model.SyncBlockedGraphMissing, Since: h.now(), Detail: h.graphDir})
		return nil
	}
	release, err := h.takeSyncLock(ctx, cmd.Explicit)
	if err != nil {
		return err
	}
	if release == nil {
		if cmd.OnSkipped != nil {
			cmd.OnSkipped()
		}
		return h.reportRecorded(ctx)
	}
	defer release()
	paused, err := h.syncPaused(ctx)
	if err != nil {
		return err
	}
	if !paused || cmd.Explicit {
		err = h.syncRound(ctx, cmd)
		if err != nil {
			return err
		}
	}
	return h.reportRecorded(ctx)
}

func (h *Handler) syncRound(ctx context.Context, cmd *command.SyncRoundCmd) error {
	var state model.SyncState
	var reason string
	err := h.FetchGraph(ctx, &command.FetchGraphCmd{SDDDir: cmd.SDDDir, Cooldown: cmd.Cooldown, OnFetched: func(fetched model.SyncState, why string) {
		state, reason = fetched, why
	}})
	if err != nil {
		return err
	}
	switch state {
	case model.SyncStateSkipped:
		return nil
	case model.SyncStateNoRepo:
		h.reportBlocked(ctx, model.SyncBlocked{Kind: model.SyncBlockedGraphMissing, Since: h.now(), Detail: h.graphDir})
		return nil
	case model.SyncStateNoRemote:
		return h.recordBlocked(ctx, model.SyncBlocked{Kind: model.SyncBlockedNoUpstream, Detail: "no remote configured"})
	case model.SyncStateNoUpstream:
		return h.recordBlocked(ctx, model.SyncBlocked{Kind: model.SyncBlockedNoUpstream})
	case model.SyncStateFetchFailed:
		return h.recordBlocked(ctx, model.SyncBlocked{Kind: model.SyncBlockedFetchFailed, Detail: reason})
	}
	blocked, err := h.pullUpstream(ctx)
	if err != nil || blocked {
		return err
	}
	blocked, err = h.publish(ctx)
	if err != nil || blocked {
		return err
	}
	return h.clearRecord(func(model.SyncBlocked) bool { return true })
}

// pullUpstream merges upstream graph commits by the pull rules and records a
// held pull or a conflict; blocked reports either.
func (h *Handler) pullUpstream(ctx context.Context) (bool, error) {
	held := h.heldPull(ctx)
	verdict, err := h.mergeUpstream(ctx, held.cmd)
	var conflict *MergeConflictError
	if errors.As(err, &conflict) {
		return true, h.recordConflict(ctx, conflict.MergeConflict)
	}
	if err != nil {
		return false, err
	}
	kind, blocked := model.HeldPullBlock(verdict)
	if !blocked {
		return false, nil
	}
	return true, h.recordBlocked(ctx, model.SyncBlocked{Kind: kind, Upstream: held.upstream, Paths: held.outside})
}

type heldPull struct {
	cmd      *command.PullGraphCmd
	upstream string
	outside  []string
}

func (h *Handler) heldPull(ctx context.Context) *heldPull {
	held := &heldPull{}
	held.cmd = h.reportingPull(ctx)
	held.cmd.OnHeld = func(_ model.PullVerdict, upstream string, outside []string) {
		held.upstream, held.outside = upstream, outside
	}
	return held
}

// publish pushes the graph commits ahead of upstream and records what holds
// them back. A push the upstream rejects is retried once after merging what
// upstream gained, by the pull rules (20261027-221041-d-tac-8b7).
func (h *Handler) publish(ctx context.Context) (bool, error) {
	var heldVerdict model.PushVerdict
	var heldUpstream string
	push := h.reportingPush(ctx)
	push.OnHeld = func(verdict model.PushVerdict, upstream string) {
		heldVerdict, heldUpstream = verdict, upstream
	}
	pushErr := h.PushGraph(ctx, push)
	if pushErr != nil {
		blocked, err := h.retryPush(ctx, push, pushErr)
		if err != nil || blocked {
			return blocked, err
		}
	}
	kind, blocked := model.HeldPushBlock(heldVerdict)
	if !blocked {
		return false, nil
	}
	return true, h.recordBlocked(ctx, model.SyncBlocked{Kind: kind, Upstream: heldUpstream})
}

func (h *Handler) retryPush(ctx context.Context, push *command.PushGraphCmd, pushErr error) (bool, error) {
	failed := func(cause error) (bool, error) {
		return true, h.recordBlocked(ctx, model.SyncBlocked{Kind: model.SyncBlockedPushFailed, Detail: cause.Error()})
	}
	if h.graphPuller == nil {
		return failed(pushErr)
	}
	err := h.graphPuller.Fetch(ctx)
	if err != nil {
		return failed(errors.Join(pushErr, err))
	}
	blocked, err := h.pullUpstream(ctx)
	if err != nil || blocked {
		return blocked, err
	}
	err = h.PushGraph(ctx, push)
	if err != nil {
		return failed(err)
	}
	return false, nil
}

// publishGraph publishes what a graph command committed, waiting for the
// sync lock. While a conflict pauses sync the commit stays local and the
// command succeeds; a blocked push is recorded and reported, not failed.
func (h *Handler) publishGraph(ctx context.Context) error {
	if h.syncMode != model.SyncModeAuto {
		return nil
	}
	release, err := h.takeSyncLock(ctx, true)
	if err != nil {
		return err
	}
	defer release()
	paused, err := h.syncPaused(ctx)
	if err != nil {
		return err
	}
	if !paused {
		blocked, err := h.publish(ctx)
		if err != nil {
			return err
		}
		if !blocked {
			err = h.clearRecord(model.SyncBlocked.ClearedByPush)
			if err != nil {
				return err
			}
		}
	}
	return h.reportRecorded(ctx)
}

// takeSyncLock returns the lock's release, nil when wait is false and another
// process holds it.
func (h *Handler) takeSyncLock(ctx context.Context, wait bool) (func(), error) {
	if h.syncLock == nil {
		return func() {}, nil
	}
	var release func()
	var err error
	if wait {
		release, err = h.syncLock.Lock(ctx)
	} else {
		release, err = h.syncLock.TryLock(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("taking the sync lock: %w", err)
	}
	return release, nil
}

// syncPaused reports whether a recorded conflict still pauses automatic
// sync, removing the record once its source commit is in the branch.
func (h *Handler) syncPaused(ctx context.Context) (bool, error) {
	current, err := h.loadRecord()
	if err != nil || current == nil || !current.Pauses() {
		return false, err
	}
	if h.graphPuller == nil {
		return true, nil
	}
	resolved, err := h.graphPuller.Contains(ctx, current.Source)
	if err != nil {
		return false, fmt.Errorf("checking whether the conflict was merged: %w", err)
	}
	if !resolved {
		return true, nil
	}
	return false, h.clearRecord(func(recorded model.SyncBlocked) bool {
		return recorded.Kind == current.Kind && recorded.Source == current.Source
	})
}

func (h *Handler) recordConflict(ctx context.Context, conflict model.MergeConflict) error {
	source, err := h.graphPuller.RevParse(ctx, conflict.Upstream)
	if err != nil {
		return err
	}
	destination, err := h.graphPuller.RevParse(ctx, "HEAD")
	if err != nil {
		return err
	}
	return h.recordBlocked(ctx, model.SyncBlocked{
		Kind: model.SyncBlockedConflict, Upstream: conflict.Upstream, Paths: conflict.Paths, Source: source, Destination: destination,
	})
}

// recordBlocked records block, keeping the start and owner of the same block
// found earlier, and reports it.
func (h *Handler) recordBlocked(ctx context.Context, block model.SyncBlocked) error {
	block.Since = h.now()
	if block.Repo == "" {
		block.Repo = h.graphDir
	}
	if h.syncRecord == nil || !block.Persisted() {
		h.reportBlocked(ctx, block)
		return nil
	}
	renewed := block
	err := h.syncRecord.Update(func(current *model.SyncBlocked) *model.SyncBlocked {
		renewed = block.Renew(current)
		return &renewed
	})
	if err != nil {
		return fmt.Errorf("recording the blocked sync: %w", err)
	}
	h.reportBlocked(ctx, renewed)
	return nil
}

// clearRecord removes the record when cleared holds for it.
func (h *Handler) clearRecord(cleared func(model.SyncBlocked) bool) error {
	if h.syncRecord == nil {
		return nil
	}
	err := h.syncRecord.Update(func(current *model.SyncBlocked) *model.SyncBlocked {
		if current == nil || cleared(*current) {
			return nil
		}
		return current
	})
	if err != nil {
		return fmt.Errorf("clearing the blocked sync: %w", err)
	}
	return nil
}

func (h *Handler) loadRecord() (*model.SyncBlocked, error) {
	if h.syncRecord == nil {
		return nil, nil
	}
	current, err := h.syncRecord.Load()
	if err != nil {
		return nil, fmt.Errorf("reading the blocked sync: %w", err)
	}
	return current, nil
}

func (h *Handler) reportRecorded(ctx context.Context) error {
	current, err := h.loadRecord()
	if err != nil || current == nil {
		return err
	}
	h.reportBlocked(ctx, *current)
	return nil
}

// ClaimSyncNotice delivers the blocked-sync notice to cmd.Session, which owns
// the fix when no session does yet.
func (h *Handler) ClaimSyncNotice(_ context.Context, cmd *command.ClaimSyncNoticeCmd) error {
	if h.syncRecord == nil {
		return nil
	}
	var notice string
	err := h.syncRecord.Update(func(current *model.SyncBlocked) *model.SyncBlocked {
		if current == nil {
			return nil
		}
		claimed := current.Claim(cmd.Session)
		notice = claimed.NoticeFor(cmd.Session)
		return &claimed
	})
	if err != nil {
		return fmt.Errorf("claiming the blocked sync: %w", err)
	}
	if notice != "" && cmd.OnNotice != nil {
		cmd.OnNotice(notice)
	}
	return nil
}

type syncReportKey struct{}

type syncReport struct {
	mu       sync.Mutex
	reported map[model.SyncBlockedKind]bool
	emit     func(model.SyncBlocked)
}

// WithSyncReport scopes blocked-sync reporting to one invocation: emit
// receives each blocked state once, however many sync paths the invocation
// runs (20261028-204922-d-tac-gcl). Without one, blocks are logged at info.
func WithSyncReport(ctx context.Context, emit func(model.SyncBlocked)) context.Context {
	return context.WithValue(ctx, syncReportKey{}, &syncReport{reported: map[model.SyncBlockedKind]bool{}, emit: emit})
}

func (h *Handler) reportBlocked(ctx context.Context, block model.SyncBlocked) {
	report, ok := ctx.Value(syncReportKey{}).(*syncReport)
	if !ok {
		slogutils.FromContext(ctx).Info(block.Banner())
		return
	}
	report.mu.Lock()
	defer report.mu.Unlock()
	if report.reported[block.Kind] {
		return
	}
	report.reported[block.Kind] = true
	report.emit(block)
}
