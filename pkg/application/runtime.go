package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/embed"
	"github.com/networkteam/slogutils"
)

// ProjectRuntime owns the immutable ports and project configuration resolved
// for one application operation.
type ProjectRuntime struct {
	options ProjectRuntimeOptions
	// connected is shared by the copies a read takes of the runtime.
	connected *connectedFailures
}

type ProjectRuntimeOptions struct {
	Project       ProjectRef
	DefaultBranch string
	Language      string
	Dependencies  []RepoID
	Graph         GraphStore
	Targets       TargetAcquirer
	Branches      BranchValidator
	// Base derives an unbound session's base branch; nil means the
	// configured default.
	Base BaseBranchResolver
	// Embedder and LLM are the two model dependencies, each a pkg/llm port
	// injected as an instance that arrives already composed — observed,
	// bounded, and rate-limited by the host's decorators. Routing, deadlines,
	// and recording are the host's composition duty (20260830-234501-d-cpt-q6n,
	// 20260902-114838-d-tac-cov); application contributes only the facts it
	// alone holds: the Purpose and the prompts, or the texts.
	Embedder    embed.Embedder
	SearchIndex SearchIndexStore
	LLM         llm.Runner
	Finalizers  []MutationFinalizer
	// Events is the project event store; nil leaves the project without one.
	Events EventStore
	// Relevance is the relevance verdict ledger; nil stores no verdicts.
	Relevance RelevanceStore
	// Sync runs automatic graph sync before a session acts on a branch and
	// serves what blocks it; nil leaves branches as they are.
	Sync GraphSync
	// Connected runs the sync rounds of the connected repositories the
	// project reaches; nil syncs none.
	Connected ConnectedSync
	// ExcludeEmbeddedFromIndex mirrors the CLI's excludeEmbedded semantics for
	// the vector index: connected-repo runtimes set it so binary-shipped base
	// entries embed once per machine (in the base store) rather than once per
	// connected repo. The base runtime leaves it false — its store includes
	// embedded entries. The rule is applied identically at index and read time.
	ExcludeEmbeddedFromIndex bool
	// Grounding is the project's grounding policy; the zero value leaves
	// grounding off with the default limits.
	Grounding GroundingPolicy
}

// GraphSync runs automatic sync of the graph repository behind a branch's
// checkout (20261027-194536-d-tac-190) and serves what blocks it
// (20261028-201909-d-tac-fxp).
type GraphSync interface {
	// SyncGraph runs one sync round for branch, skipping it when another
	// process is syncing.
	SyncGraph(ctx context.Context, branch string, trigger SyncTrigger) error
	// SyncNotice returns the blocked-sync notice for session, empty while
	// sync proceeds; the first session to ask owns the fix.
	SyncNotice(ctx context.Context, branch, session string) (string, error)
	// ReleaseSyncOwner gives up session's ownership of branch's blocked sync
	// as the session ends, returning the banner while sync is blocked.
	ReleaseSyncOwner(ctx context.Context, branch, session string) (string, error)
	// SyncDue reports whether a sync round of branch has an operation due:
	// automatic sync is on and the git gate would admit its fetch, or its
	// push with commits waiting.
	SyncDue(ctx context.Context, branch string) (bool, error)
	// SyncBlocked reads what blocks sync of branch, nil while sync proceeds.
	SyncBlocked(ctx context.Context, branch string) (*SyncBlocked, error)
	// BackfillTitles titles the untitled entries of branch's graph through
	// runner and records a failure for the sync notice
	// (20261105-233807-d-tac-831); it returns at once while another backfill
	// runs in the checkout.
	BackfillTitles(ctx context.Context, branch string, trigger SyncTrigger, runner llm.Runner) error
}

// SyncBlocked is what blocks automatic sync of a graph repository, since
// when, and the session owning the fix. A conflict also names the upstream
// commit that would not merge (Source) and the local commit it was to merge
// into (Destination).
type SyncBlocked struct {
	Kind        string
	Since       time.Time
	Upstream    string
	Detail      string
	Source      string
	Destination string
	Paths       []string
	Owner       string
	// Steps tells the owning agent how to clear the block.
	Steps string
}

// SyncTrigger is what starts a sync round.
type SyncTrigger string

const (
	// SyncOnCall: an engine call; its title backfill waits out the backfill
	// cooldown.
	SyncOnCall SyncTrigger = "call"
	// SyncOnTimer: the timer of a running server, once a round is due.
	SyncOnTimer SyncTrigger = "timer"
	// SyncOnSessionStart: a session opening; it starts the title backfill
	// alone, which always runs.
	SyncOnSessionStart SyncTrigger = "session-start"
)

// titleBackfillTimeout bounds one background title backfill, whose title
// calls the injected runner bounds in turn.
const titleBackfillTimeout = 10 * time.Minute

// syncGraph runs a sync round for branch, the base branch when empty. A
// sync failure never blocks the session it precedes; it is logged.
func (r *ProjectRuntime) syncGraph(ctx context.Context, branch string, trigger SyncTrigger) {
	if r.options.Sync == nil {
		return
	}
	branch, ok := r.syncBranch(ctx, branch)
	if !ok {
		return
	}
	err := r.options.Sync.SyncGraph(ctx, branch, trigger)
	if err != nil {
		slogutils.FromContext(ctx).Warn(fmt.Sprintf("sync: %v", err), "branch", branch)
	}
}

// backfillTitles starts the title backfill of branch's graph in the
// background, the base branch when empty: the caller is served without
// waiting for it. Its failures reach the sync notice through the record; only
// a failure to run it at all is logged.
func (r *ProjectRuntime) backfillTitles(ctx context.Context, branch string, trigger SyncTrigger) {
	if r.options.Sync == nil {
		return
	}
	branch, ok := r.syncBranch(ctx, branch)
	if !ok {
		return
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		ctx, cancel := context.WithTimeout(ctx, titleBackfillTimeout)
		defer cancel()
		err := r.options.Sync.BackfillTitles(ctx, branch, trigger, r.options.LLM)
		if err != nil {
			slogutils.FromContext(ctx).Warn(fmt.Sprintf("title backfill: %v", err), "branch", branch)
		}
	}()
}

// syncNotice returns the blocked-sync notice of branch's graph repository
// for session, followed by the connected repositories whose last round
// failed.
func (r *ProjectRuntime) syncNotice(ctx context.Context, branch, session string) string {
	notices := make([]string, 0, 2)
	for _, notice := range []string{r.graphSyncNotice(ctx, branch, session), r.connected.notice()} {
		if notice != "" {
			notices = append(notices, notice)
		}
	}
	return strings.Join(notices, "\n")
}

func (r *ProjectRuntime) graphSyncNotice(ctx context.Context, branch, session string) string {
	if r.options.Sync == nil {
		return ""
	}
	branch, ok := r.syncBranch(ctx, branch)
	if !ok {
		return ""
	}
	notice, err := r.options.Sync.SyncNotice(ctx, branch, session)
	if err != nil {
		slogutils.FromContext(ctx).Warn(fmt.Sprintf("sync: reading the blocked state: %v", err), "branch", branch)
	}
	return notice
}

func (r *ProjectRuntime) releaseSyncOwner(ctx context.Context, branch, session string) string {
	if r.options.Sync == nil {
		return ""
	}
	branch, ok := r.syncBranch(ctx, branch)
	if !ok {
		return ""
	}
	notice, err := r.options.Sync.ReleaseSyncOwner(ctx, branch, session)
	if err != nil {
		slogutils.FromContext(ctx).Warn(fmt.Sprintf("sync: releasing the blocked state: %v", err), "branch", branch)
	}
	return notice
}

func (r *ProjectRuntime) syncBlocked(ctx context.Context, branch string) (*SyncBlocked, error) {
	if r.options.Sync == nil {
		return nil, nil
	}
	branch, ok := r.syncBranch(ctx, branch)
	if !ok {
		return nil, nil
	}
	return r.options.Sync.SyncBlocked(ctx, branch)
}

func (r *ProjectRuntime) syncBranch(ctx context.Context, branch string) (string, bool) {
	if branch != "" {
		return branch, true
	}
	target, source, err := r.baseTarget(ctx)
	if err != nil {
		slogutils.FromContext(ctx).Warn("sync: resolving the base branch", "err", err)
		return "", false
	}
	// A serving checkout outside Git or on a detached HEAD has no branch to sync.
	if source == BaseWithoutBranch {
		return "", false
	}
	return target.Branch, true
}

// syncTick is how often a running server asks the git gate whether its
// graph's sync round is due; the gate's cooldowns set the pace
// (20261107-095427-d-tac-yux).
const syncTick = time.Minute

// RunSyncTimer runs, whenever a tick finds it due, the base branch's sync
// round with its title backfill and the round of every connected repository
// the project reaches, until ctx ends.
func (r *ProjectRuntime) RunSyncTimer(ctx context.Context) error {
	branch, own := "", false
	if r.options.Sync != nil {
		branch, own = r.syncBranch(ctx, "")
	}
	if !own && r.options.Connected == nil {
		return nil
	}
	ticker := time.NewTicker(syncTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if own {
				r.syncGraphWhenDue(ctx, branch)
			}
			r.syncDueRepos(ctx)
		}
	}
}

func (r *ProjectRuntime) syncGraphWhenDue(ctx context.Context, branch string) {
	due, err := r.options.Sync.SyncDue(ctx, branch)
	if err != nil {
		slogutils.FromContext(ctx).Warn(fmt.Sprintf("sync: %v", err), "branch", branch)
		return
	}
	if due {
		r.syncGraph(ctx, branch, SyncOnTimer)
		r.backfillTitles(ctx, branch, SyncOnTimer)
	}
}

func NewProjectRuntime(options ProjectRuntimeOptions) (*ProjectRuntime, error) {
	if options.Project.ID == "" {
		return nil, fmt.Errorf("sdd: project ID is required")
	}
	if options.Graph == nil {
		return nil, fmt.Errorf("sdd: GraphStore is required")
	}
	if (options.Embedder == nil) != (options.SearchIndex == nil) {
		return nil, fmt.Errorf("sdd: Embedder and SearchIndexStore must be configured together")
	}
	if options.LLM == nil {
		return nil, fmt.Errorf("sdd: LLM runner is required")
	}
	if options.Targets == nil && options.DefaultBranch != "" {
		options.Targets = FixedTargetAcquirer{
			Target: MutationTarget{Project: options.Project.ID, Branch: options.DefaultBranch},
			Graph:  options.Graph, Finalizers: options.Finalizers,
		}
	}
	return &ProjectRuntime{options: options, connected: &connectedFailures{}}, nil
}

func (r *ProjectRuntime) Project() ProjectRef {
	if r == nil {
		return ProjectRef{}
	}
	return r.options.Project
}

func (r *ProjectRuntime) defaultMutationTarget() (MutationTarget, error) {
	target := MutationTarget{Project: r.options.Project.ID, Branch: strings.TrimSpace(r.options.DefaultBranch)}
	if err := target.Validate(r.options.Project.ID); err != nil {
		return MutationTarget{}, fmt.Errorf("sdd: project %s has no concrete default mutation branch configured: %w", r.options.Project.ID, err)
	}
	return target, nil
}

// baseTarget resolves the target an unbound write goes to: the branch the
// composition derives, else the configured default.
func (r *ProjectRuntime) baseTarget(ctx context.Context) (MutationTarget, BaseSource, error) {
	if r.options.Base == nil {
		target, err := r.defaultMutationTarget()
		return target, BaseFromDefault, err
	}
	branch, err := r.options.Base.BaseBranch(ctx)
	if err != nil {
		return MutationTarget{}, "", fmt.Errorf("sdd: deriving the base branch of project %s: %w", r.options.Project.ID, err)
	}
	if branch == "" {
		target, err := r.defaultMutationTarget()
		return target, BaseWithoutBranch, err
	}
	target := MutationTarget{Project: r.options.Project.ID, Branch: branch}
	if err := target.Validate(r.options.Project.ID); err != nil {
		return MutationTarget{}, "", err
	}
	return target, BaseFromCheckout, nil
}

func (r *ProjectRuntime) acquire(ctx context.Context, target MutationTarget) (*AcquiredTarget, error) {
	if err := target.Validate(r.options.Project.ID); err != nil {
		return nil, markTargetAcquisitionError(target, err)
	}
	if r.options.Targets == nil {
		return nil, markTargetAcquisitionError(target, &ApplicationError{Code: ErrorWriteDenied, Message: fmt.Sprintf("project %s has no mutation target acquirer for branch %q", target.Project, target.Branch)})
	}
	acquired, err := r.options.Targets.Acquire(ctx, target)
	if err != nil {
		return nil, markTargetAcquisitionError(target, err)
	}
	if err := acquired.validate(target); err != nil {
		if acquired != nil && acquired.Release != nil {
			return nil, markTargetAcquisitionError(target, errors.Join(err, acquired.Release()))
		}
		return nil, markTargetAcquisitionError(target, err)
	}
	return acquired, nil
}
