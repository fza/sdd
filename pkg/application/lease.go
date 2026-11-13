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

// GraphDemand is what renewing the lease of a graph for a call did
// (20261113-232855-d-tac-1nz): Ran when the lease start ran a sync round,
// Pending when that round still runs past the read wait, with when the graph
// last fetched.
type GraphDemand struct {
	Ran     bool
	Pending bool
	Fetched time.Time
}

// refresh is one sync round a call stopped waiting for.
type refresh struct {
	project ProjectID
	fetched time.Time
	shown   bool
	settled chan struct{}
}

// refreshes holds the sync rounds still running past the calls that started
// them, for the sync notice.
type refreshes struct {
	mu      sync.Mutex
	running map[*refresh]struct{}
}

func (r *refreshes) begin(project ProjectID) *refresh {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running == nil {
		r.running = map[*refresh]struct{}{}
	}
	pending := &refresh{project: project, settled: make(chan struct{})}
	r.running[pending] = struct{}{}
	return pending
}

// show lists pending in the notice unless it settled already; shown reports
// whether it is listed.
func (r *refreshes) show(pending *refresh, fetched time.Time) (shown bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, running := r.running[pending]; !running {
		return false
	}
	pending.fetched, pending.shown = fetched, true
	return true
}

func (r *refreshes) end(pending *refresh) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, running := r.running[pending]; running {
		delete(r.running, pending)
		close(pending.settled)
	}
}

func (r *refreshes) notice(now time.Time) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var lines []string
	for pending := range r.running {
		if !pending.shown {
			continue
		}
		age := "never fetched"
		if !pending.fetched.IsZero() {
			age = fmt.Sprintf("fetched %s, %s ago", pending.fetched.Local().Format("15:04"), now.Sub(pending.fetched).Round(time.Minute))
		}
		lines = append(lines, fmt.Sprintf("refresh of %s still running; served its graph as it stands (%s)", pending.project, age))
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}

// demand starts or renews the lease of runtime's graph on branch for a call
// and runs the sync round a lease start asks for, waiting for it at most the
// read wait; ran reports such a round. A round still running then shows in
// the sync notice until it ends, and Shutdown waits for it. A failure never
// blocks the call; it is logged.
func (a *Application) demand(ctx context.Context, runtime *ProjectRuntime, branch string) (ran bool) {
	if runtime == nil || runtime.options.Sync == nil {
		return false
	}
	branch, ok := runtime.syncBranch(ctx, branch)
	if !ok {
		return false
	}
	pending := a.refreshes.begin(runtime.options.Project.ID)
	demand, err := runtime.options.Sync.DemandGraph(ctx, branch, func() { a.refreshes.end(pending) })
	if err != nil {
		slogutils.FromContext(ctx).Warn(fmt.Sprintf("sync: %v", err), "branch", branch)
	}
	if !demand.Pending || !a.refreshes.show(pending, demand.Fetched) {
		a.refreshes.end(pending)
		return demand.Ran
	}
	a.background.start(func() { <-pending.settled })
	return demand.Ran
}
