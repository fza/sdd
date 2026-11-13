package handlers

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

// CloneRound runs the sync round of an sdd-owned clone: the one path every
// connected repository refreshes through, read or written
// (20261107-095427-d-tac-yux).
type CloneRound interface {
	SyncClone(ctx context.Context, dir string, cmd *command.SyncRoundCmd) error
	// TimerRound is what the serve timer runs for the clone at dir.
	TimerRound(ctx context.Context, dir string) (model.TimerRound, error)
}

var errNoCloneRound = errors.New("connected-repo sync is not wired (handler built without CloneRound)")

// connectedSync says how far one connected repo's sync goes.
type connectedSync struct {
	// clone makes the clone when the repo has none.
	clone bool
	// timer runs what the serve timer has due for the clone, and reports
	// nothing otherwise.
	timer bool
	// explicit waits for the sync lock and runs while a conflict pauses
	// automatic sync.
	explicit bool
	onPhase  func(model.Phase)
}

// RepoSync executes a RepoSyncCmd: the sync round of each selected connected
// repo through its sdd-owned clone, at most the gate's concurrency per host
// at once.
func (h *Handler) RepoSync(ctx context.Context, cmd *command.RepoSyncCmd) error {
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
	cfg, err := h.repos.Registry().Load()
	if err != nil {
		return err
	}
	targets := cmd.RepoIDs
	mode := connectedSync{clone: true, explicit: true}
	if !cmd.Due {
		ctx = gitgate.WithCredits(ctx, gitgate.Fetch, gitgate.Push)
	}
	switch {
	case cmd.Due:
		targets, err = h.closureMembers(ctx)
		if err != nil {
			return err
		}
		mode = connectedSync{timer: true}
	case len(targets) == 0:
		for _, r := range cfg.Repos {
			targets = append(targets, r.RepoID)
		}
		if len(targets) == 0 {
			return fmt.Errorf("no connected repos — add one with `sdd repo add <clone-url>`")
		}
	}

	outcomes := make([]*model.RepoSyncOutcome, len(targets))
	slots := map[string]chan struct{}{}
	var wg sync.WaitGroup
	for i, repoID := range targets {
		repo, connected := cfg.Connected(repoID)
		if !connected {
			if !cmd.Due {
				outcomes[i] = &model.RepoSyncOutcome{RepoID: repoID, State: model.RepoRefused, Reason: "not connected"}
			}
			continue
		}
		host, size, err := h.repos.Slots(ctx, repo)
		if err != nil {
			outcomes[i] = h.failedOutcome(ctx, repo, err)
			continue
		}
		slot, ok := slots[host]
		if !ok {
			slot = make(chan struct{}, size)
			slots[host] = slot
		}
		wg.Go(func() {
			slot <- struct{}{}
			defer func() { <-slot }()
			outcome, reported := h.syncConnected(ctx, repo, mode)
			if reported {
				outcomes[i] = &outcome
			}
		})
	}
	wg.Wait()
	for _, outcome := range outcomes {
		if outcome != nil {
			cmd.OnOutcome(*outcome)
		}
	}
	return nil
}

// closureMembers lists the connected repos the local repo's dependency
// closure reaches, as their clones stand.
func (h *Handler) closureMembers(ctx context.Context) ([]model.RepoID, error) {
	root, err := h.reader.LocalRepoID()
	if err != nil {
		return nil, err
	}
	var members []model.RepoID
	for repoID, err := range model.DependencyClosure(ctx, root, model.DependencyResolverFunc[model.RepoID](h.reader.DeclaredDependencies)) {
		if err != nil {
			return nil, err
		}
		if repoID != root {
			members = append(members, repoID)
		}
	}
	return members, nil
}

// syncConnected brings one connected repo's clone up to date as mode says;
// reported is false when mode leaves the repo alone.
func (h *Handler) syncConnected(ctx context.Context, repo repos.ConnectedRepo, mode connectedSync) (outcome model.RepoSyncOutcome, reported bool) {
	outcome = model.RepoSyncOutcome{RepoID: repo.RepoID, State: model.RepoSynced}
	dir, err := h.repos.Registry().CloneDir(repo.RepoID)
	if err != nil {
		return *h.failedOutcome(ctx, repo, err), true
	}
	if !repos.IsCloned(dir) {
		if !mode.clone {
			return outcome, false
		}
		emitPhase(mode.onPhase, model.PhaseConnecting)
		emitPhase(mode.onPhase, model.PhaseCloning)
		_, _, err = h.repos.Ensure(ctx, repo)
		deferred, isDeferred := deferral(err)
		if isDeferred {
			return model.RepoSyncOutcome{RepoID: repo.RepoID, State: model.RepoSkipped, Next: deferred.Next}, true
		}
		if err != nil {
			return *h.failedOutcome(ctx, repo, err), true
		}
		return outcome, true
	}
	var pushOnly bool
	if mode.timer {
		round, err := h.cloneRound.TimerRound(ctx, dir)
		if err != nil {
			return *h.failedOutcome(ctx, repo, err), true
		}
		if round == model.TimerRoundNone {
			return outcome, false
		}
		pushOnly = round == model.TimerRoundPush
	}
	if mode.explicit {
		_, _, err = h.repos.Ensure(ctx, repo)
		if err != nil {
			return *h.failedOutcome(ctx, repo, err), true
		}
	}
	emitPhase(mode.onPhase, model.PhaseSyncing)
	var mu sync.Mutex
	var blocks []model.SyncBlocked
	roundCtx := WithSyncReport(ctx, func(block model.SyncBlocked) {
		mu.Lock()
		defer mu.Unlock()
		blocks = append(blocks, block)
	})
	var skipped, pushed bool
	var next time.Time
	err = h.cloneRound.SyncClone(roundCtx, dir, &command.SyncRoundCmd{
		Explicit:  mode.explicit,
		PushOnly:  pushOnly,
		OnSkipped: func() { skipped = true },
		OnDeferred: func(_ string, at time.Time) {
			if next.IsZero() || at.Before(next) {
				next = at
			}
		},
		OnPushed: func(int) { pushed = true },
	})
	if err != nil {
		return *h.failedOutcome(ctx, repo, err), true
	}
	for _, block := range blocks {
		if !block.TitlesFailed() {
			return *h.failed(ctx, repo, block.Reason()), true
		}
	}
	switch {
	case skipped:
		return outcome, !mode.timer
	case pushed:
		outcome.State = model.RepoPushed
	case !next.IsZero():
		outcome.State, outcome.Next = model.RepoSkipped, next
	}
	return outcome, true
}

// failedOutcome reports cause as repo's failure.
func (h *Handler) failedOutcome(ctx context.Context, repo repos.ConnectedRepo, cause error) *model.RepoSyncOutcome {
	return h.failed(ctx, repo, model.GitFailureDetail(cause))
}

// failed reports repo as failed for reason, with when the gate next admits
// its fetch.
func (h *Handler) failed(ctx context.Context, repo repos.ConnectedRepo, reason string) *model.RepoSyncOutcome {
	outcome := &model.RepoSyncOutcome{RepoID: repo.RepoID, State: model.RepoFailed, Reason: reason}
	_, next, err := h.repos.FetchDue(ctx, repo)
	if err != nil {
		slogutils.FromContext(ctx).Debug("sync: reading when a failed repo is retried", "repo", repo.RepoID, "err", err)
		return outcome
	}
	outcome.Next = next
	return outcome
}

// EnsureReposFresh starts or renews the lease of each named connected repo
// for a read (20261113-232855-d-tac-1nz): a clone on first use; else, when
// the lease starts past the repo's effective fetch cooldown, one sync round,
// waited for at most read_wait. A round still running then finishes in the
// background: cmd.OnPending reports it, cmd.OnSettled its outcome. A
// deferred round leaves the clone as it stands. Unconnected repo IDs are
// skipped — the read side renders them unresolved, which is that state's
// honest surface. Reports whether any clone changed so a long-lived caller
// can invalidate its GraphSource; every repo is tried, and the failures
// return joined.
func (h *Handler) EnsureReposFresh(ctx context.Context, cmd command.EnsureReposFreshCmd) (changed bool, err error) {
	if len(cmd.RepoIDs) == 0 {
		return false, nil
	}
	if h.repos == nil {
		return false, errNoRepos
	}
	if h.cloneRound == nil {
		return false, errNoCloneRound
	}
	cfg, err := h.repos.Registry().Load()
	if err != nil {
		return false, err
	}
	logger := slogutils.FromContext(ctx)
	var failures []error
	for _, repoID := range cmd.RepoIDs {
		repo, ok := cfg.Connected(repoID)
		if !ok {
			logger.Debug("repo not connected; skipping its refresh", "repo", repoID)
			continue
		}
		outcome, reported, err := h.demandConnected(ctx, repo, cmd)
		switch {
		case err != nil:
			failures = append(failures, fmt.Errorf("%s: %w", repoID, err))
		case !reported:
			logger.Debug("refresh not due", "repo", repoID)
		case outcome.State == model.RepoFailed:
			failures = append(failures, fmt.Errorf("%s: %s", repoID, outcome.Reason))
		case outcome.State == model.RepoSkipped:
			logger.Debug("refresh deferred", "repo", repoID, "next", outcome.Next)
		default:
			changed = true
		}
	}
	return changed, errors.Join(failures...)
}

// demandConnected clones repo on first use, then renews its lease and runs
// the round a lease start asks for; reported is false when none runs or it
// still runs past the read wait.
func (h *Handler) demandConnected(ctx context.Context, repo repos.ConnectedRepo, cmd command.EnsureReposFreshCmd) (model.RepoSyncOutcome, bool, error) {
	dir, err := h.repos.Registry().CloneDir(repo.RepoID)
	if err != nil {
		return model.RepoSyncOutcome{}, false, err
	}
	if !repos.IsCloned(dir) {
		outcome, reported := h.syncConnected(ctx, repo, connectedSync{clone: true, onPhase: cmd.OnPhase})
		_, err = h.repos.Demand(ctx, repo)
		return outcome, reported, err
	}
	demand, err := h.repos.Demand(ctx, repo)
	if err != nil || !demand.Sync {
		return model.RepoSyncOutcome{}, false, err
	}
	outcome, finished := waitFor(ctx, h, demand.Wait, func(ctx context.Context) model.RepoSyncOutcome {
		outcome, _ := h.syncConnected(ctx, repo, connectedSync{onPhase: cmd.OnPhase})
		return outcome
	}, func() {
		if cmd.OnPending != nil {
			cmd.OnPending(repo.RepoID, demand.Fetched)
		}
	}, func(outcome model.RepoSyncOutcome) {
		if cmd.OnSettled != nil {
			cmd.OnSettled(outcome)
		}
	})
	return outcome, finished, nil
}

// waitFor runs run on a context that outlives ctx and waits for it at most
// wait. Past that it calls pending and returns, and run finishes in the
// background, handing its result to settled, never before pending; Settle
// waits for it.
func waitFor[T any](ctx context.Context, h *Handler, wait time.Duration, run func(context.Context) T, pending func(), settled func(T)) (result T, finished bool) {
	var mu sync.Mutex
	waiting := true
	done := make(chan T, 1)
	h.background.Add(1)
	go func() {
		defer h.background.Done()
		result := run(context.WithoutCancel(ctx))
		mu.Lock()
		defer mu.Unlock()
		if waiting {
			done <- result
			return
		}
		settled(result)
	}()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case result := <-done:
		return result, true
	case <-timer.C:
	}
	mu.Lock()
	defer mu.Unlock()
	select {
	case result := <-done:
		return result, true
	default:
	}
	waiting = false
	pending()
	return result, false
}

// Settle waits for the sync rounds that outlived the calls starting them, so
// a process ending does not cut one short.
func (h *Handler) Settle() {
	h.background.Wait()
}

// freshenReferencedRepos refreshes the clones of every repo a ref set points
// into, as far as the git gate admits, so capture-time resolution reads live
// state. Unconnected repos are skipped — resolve-or-block reports them. The
// refresh is optional (20261027-150208-d-tac-bg4): a failed one is a warning,
// and resolution reads the clones as they stand.
func (h *Handler) freshenReferencedRepos(ctx context.Context, refs []model.Ref) {
	repoIDs := model.CrossRepoIDs(model.RefIDs(refs))
	if len(repoIDs) == 0 {
		return
	}
	logger := slogutils.FromContext(ctx)
	_, err := h.EnsureReposFresh(ctx, command.EnsureReposFreshCmd{RepoIDs: repoIDs, OnPending: func(repoID model.RepoID, fetched time.Time) {
		logger.Warn("refresh of a referenced dependency still running; resolving against its clone as it stands", "repo", repoID, "fetched", fetched)
	}})
	if err != nil {
		logger.Warn("could not refresh referenced dependencies; resolving against their clones as they stand", "repos", repoIDs, "err", err)
	}
}

// fetchOnMiss runs the sync round of any connected repo whose referenced
// backward-class target is absent from the loaded member graph, then reloads
// the graph once so pre-flight judges the freshest state. A target that is
// still absent after the round is genuinely missing — that block stands, as
// does one whose fetch the git gate defers. Forward-class refs are exempt
// from resolve-or-block, so they trigger no fetch.
func (h *Handler) fetchOnMiss(ctx context.Context, graph *model.Graph, refs []model.Ref) *model.Graph {
	logger := slogutils.FromContext(ctx)
	var stale []model.RepoID
	for _, r := range refs {
		repoID, entryID, ok := model.SplitCrossRepoID(r.ID)
		if !ok || model.IsForwardClassRefKind(r.Kind) {
			continue
		}
		member, err := graph.MemberGraph(repoID)
		if err != nil || member == nil {
			continue // unconnected or unloadable — resolve-or-block owns the finding
		}
		if _, found := member.ByID[entryID]; !found {
			stale = append(stale, repoID)
		}
	}
	if len(stale) == 0 || h.repos == nil {
		return graph
	}
	if h.cloneRound == nil {
		logger.Warn("fetch-on-miss: " + errNoCloneRound.Error())
		return graph
	}
	cfg, err := h.repos.Registry().Load()
	if err != nil {
		logger.Warn("fetch-on-miss: loading global config failed", "err", err)
		return graph
	}
	fetched := false
	for _, repoID := range stale {
		repo, ok := cfg.Connected(repoID)
		if !ok {
			continue
		}
		outcome, reported := h.syncConnected(ctx, repo, connectedSync{})
		switch {
		case !reported:
		case outcome.State == model.RepoFailed:
			logger.Warn("fetch-on-miss round failed", "repo", repoID, "reason", outcome.Reason)
		case outcome.State == model.RepoSkipped:
			logger.Debug("fetch-on-miss round deferred", "repo", repoID, "next", outcome.Next)
		default:
			fetched = true
		}
	}
	if !fetched {
		return graph
	}
	reloaded, err := h.reader.CurrentGraph(h.graphDir)
	if err != nil {
		logger.Warn("fetch-on-miss reload failed", "err", err)
		return graph
	}
	return reloaded
}
