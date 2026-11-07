package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/networkteam/slogutils"
)

// ConnectedSync runs the sync rounds of the connected repositories a project
// reaches, each through its sdd-owned clone and the git gate
// (20261107-095427-d-tac-yux).
type ConnectedSync interface {
	SyncRepos(ctx context.Context, request SyncReposRequest) ([]RepoSyncOutcome, error)
}

// SyncReposRequest selects the connected repositories a sync reaches: the
// named ones, every connected one when none is named, or with Due the
// cloned members of the project's dependency closure whose round the gate
// has due, as the timer of a running server asks.
type SyncReposRequest struct {
	Repos []RepoID
	Due   bool
}

// RepoSyncState is what a sync round did for one connected repository.
type RepoSyncState string

const (
	RepoSynced  RepoSyncState = "synced"
	RepoPushed  RepoSyncState = "pushed"
	RepoSkipped RepoSyncState = "skipped"
	RepoFailed  RepoSyncState = "failed"
	RepoRefused RepoSyncState = "refused"
)

// RepoSyncOutcome is one connected repository's sync outcome: when skipped,
// Next is when it is due; when failed, Reason says why and Next is when its
// fetch is retried.
type RepoSyncOutcome struct {
	Repo   RepoID
	State  RepoSyncState
	Next   time.Time
	Reason string
}

// SyncReposResult lists the outcome per repository, in the order requested.
type SyncReposResult struct {
	Outcomes []RepoSyncOutcome
}

// connectedFailures holds the connected repositories whose last round in
// this process failed, for the sync notice.
type connectedFailures struct {
	mu     sync.Mutex
	failed map[RepoID]RepoSyncOutcome
}

func (f *connectedFailures) note(outcomes []RepoSyncOutcome) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, outcome := range outcomes {
		switch outcome.State {
		case RepoFailed:
			if f.failed == nil {
				f.failed = map[RepoID]RepoSyncOutcome{}
			}
			f.failed[outcome.Repo] = outcome
		case RepoSynced, RepoPushed:
			delete(f.failed, outcome.Repo)
		}
	}
}

func (f *connectedFailures) notice() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	repos := make([]RepoID, 0, len(f.failed))
	for repo := range f.failed {
		repos = append(repos, repo)
	}
	slices.Sort(repos)
	lines := make([]string, 0, len(repos))
	for _, repo := range repos {
		outcome := f.failed[repo]
		line := fmt.Sprintf("sync of connected repository %s failed: %s", repo, outcome.Reason)
		if !outcome.Next.IsZero() {
			line += fmt.Sprintf("; retried from %s", outcome.Next.Local().Format("15:04"))
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// SyncRepos runs the sync round of the project's connected repositories as
// request selects them. It needs write access: a round merges into the
// clones and publishes the commits they hold.
func (a *Application) SyncRepos(ctx context.Context, identity RequestIdentity, project ProjectID, request SyncReposRequest) (SyncReposResult, error) {
	_, runtime, err := a.resolve(ctx, identity, project, AccessWrite)
	if err != nil {
		return SyncReposResult{}, err
	}
	outcomes, err := runtime.syncRepos(ctx, request)
	if err != nil {
		return SyncReposResult{}, err
	}
	return SyncReposResult{Outcomes: outcomes}, nil
}

func (r *ProjectRuntime) syncRepos(ctx context.Context, request SyncReposRequest) ([]RepoSyncOutcome, error) {
	if r.options.Connected == nil {
		return nil, &ApplicationError{Code: ErrorProjectUnavailable, Message: fmt.Sprintf("project %s syncs no connected repositories here", r.options.Project.ID)}
	}
	outcomes, err := r.options.Connected.SyncRepos(ctx, request)
	if err != nil {
		return nil, err
	}
	r.connected.note(outcomes)
	return outcomes, nil
}

// syncDueRepos runs the rounds the timer finds due; a failure reaches the
// sync notice, a deferral stays silent.
func (r *ProjectRuntime) syncDueRepos(ctx context.Context) {
	if r.options.Connected == nil {
		return
	}
	_, err := r.syncRepos(ctx, SyncReposRequest{Due: true})
	if err != nil {
		slogutils.FromContext(ctx).Warn(fmt.Sprintf("sync: connected repositories: %v", err))
	}
}
