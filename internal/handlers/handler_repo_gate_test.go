package handlers_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/repos/repostest"
	"github.com/networkteam/sdd/internal/syncstate"
)

// repoRound runs the real sync round on a scripted repository per clone
// dir, under the gate, the way syncstate.CloneRounds runs it on a clone.
type repoRound struct {
	gate  *gitgate.Gate
	mu    sync.Mutex
	repos map[string]*synctest.Repo
	runs  int
	// hold keeps each round running this long; inFlight and maxInFlight
	// count the rounds running at once.
	hold                  time.Duration
	inFlight, maxInFlight int
}

func (r *repoRound) repo(dir string) (*synctest.Repo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	repo, ok := r.repos[dir]
	if !ok {
		return nil, fmt.Errorf("no scripted repository at %s", dir)
	}
	return repo, nil
}

func (r *repoRound) SyncClone(ctx context.Context, dir string, cmd *command.SyncRoundCmd) error {
	r.mu.Lock()
	r.runs++
	r.inFlight++
	r.maxInFlight = max(r.maxInFlight, r.inFlight)
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.inFlight--
		r.mu.Unlock()
	}()
	time.Sleep(r.hold)
	repo, err := r.repo(dir)
	if err != nil {
		return err
	}
	return handlers.New(handlers.Options{
		GraphFetcher: repo, GraphPuller: repo, Pusher: repo, Gate: r.gate, SyncMode: model.SyncModeAuto,
		SyncLock: &synctest.Lock{}, SyncRecord: &synctest.Record{},
	}).SyncRound(ctx, cmd)
}

func (r *repoRound) TimerRound(ctx context.Context, dir string) (model.TimerRound, error) {
	repo, err := r.repo(dir)
	if err != nil {
		return model.TimerRoundNone, err
	}
	return syncstate.PlanTimerRound(ctx, r.gate, repo)
}

func (r *repoRound) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs
}

// connections is a handler over connected repos whose clones exist, each
// backed by a scripted repository on its own https remote, plus one repo
// connected without a clone.
type connections struct {
	handler *handlers.Handler
	manager *repos.Manager
	round   *repoRound
	git     *synctest.CloneGit
	repos   map[model.RepoID]*synctest.Repo
	// now is the gate's clock.
	now *time.Time
}

const uncloned = model.RepoID("example.test/acme/uncloned")

func newConnections(t *testing.T, now time.Time, scripted map[model.RepoID]*synctest.Repo) *connections {
	t.Helper()
	dir := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(dir, "config.yaml"), CacheRoot: filepath.Join(dir, "cache"), DataRoot: filepath.Join(dir, "data")}
	cfg := &repos.GlobalConfig{}
	var clones []string
	for repoID := range scripted {
		clones = append(clones, "https://"+string(repoID)+".git")
	}
	gate := synctest.Gate(t.TempDir(), func() time.Time { return now }, clones...)
	round := &repoRound{gate: gate, repos: map[string]*synctest.Repo{}}
	git := &synctest.CloneGit{Remote: "https://example.test/acme/uncloned.git", Remotes: map[string]string{}}
	reg := repos.NewRegistry(loc)
	for repoID, repo := range scripted {
		repo.URL = "https://" + string(repoID) + ".git"
		err := cfg.AddRepo(repos.ConnectedRepo{RepoID: repoID, CloneURL: repo.URL})
		if err != nil {
			t.Fatal(err)
		}
		cloneDir, err := reg.CloneDir(repoID)
		if err != nil {
			t.Fatal(err)
		}
		err = os.MkdirAll(filepath.Join(cloneDir, ".git"), 0o755)
		if err != nil {
			t.Fatal(err)
		}
		round.repos[cloneDir] = repo
		git.Remotes[cloneDir] = repo.URL
	}
	err := cfg.AddRepo(repos.ConnectedRepo{RepoID: uncloned, CloneURL: "https://example.test/acme/uncloned.git"})
	if err != nil {
		t.Fatal(err)
	}
	repostest.WriteConfig(t, loc.ConfigPath, cfg)
	manager := repos.NewManager(reg, repos.NewGraphClones(loc, git, gate))
	return &connections{handler: handlers.New(handlers.Options{Repos: manager, CloneRound: round}), manager: manager, round: round, git: git, repos: scripted, now: &now}
}

func (c *connections) sync(t *testing.T, cmd *command.RepoSyncCmd) map[model.RepoID]model.RepoSyncOutcome {
	t.Helper()
	outcomes := map[model.RepoID]model.RepoSyncOutcome{}
	cmd.OnOutcome = func(outcome model.RepoSyncOutcome) { outcomes[outcome.RepoID] = outcome }
	err := c.handler.RepoSync(t.Context(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	return outcomes
}

// `sdd repo sync` reports per repo what its round did: a fetched repo is
// synced, one with commits waiting is pushed, an uncloned one is cloned, a
// failing fetch is failed with when it is retried, an unconnected name is
// refused, and a repo within its cooldown is skipped with when it is due.
func TestRepoSyncReportsEachRepo(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	c := newConnections(t, now, map[model.RepoID]*synctest.Repo{
		"example.test/acme/quiet":   {Upstream: "origin/main"},
		"example.test/acme/ahead":   {Upstream: "origin/main", Ahead: 1, GraphAhead: 1},
		"example.test/acme/offline": {Upstream: "origin/main", FetchErr: errors.New("fatal: unable to access: Could not resolve host: example.test")},
	})
	first := c.sync(t, &command.RepoSyncCmd{RepoIDs: []model.RepoID{
		"example.test/acme/quiet", "example.test/acme/ahead", "example.test/acme/offline", uncloned, "example.test/acme/stranger",
	}})
	want := map[model.RepoID]model.RepoSyncState{
		"example.test/acme/quiet":    model.RepoSynced,
		"example.test/acme/ahead":    model.RepoPushed,
		"example.test/acme/offline":  model.RepoFailed,
		uncloned:                     model.RepoSynced,
		"example.test/acme/stranger": model.RepoRefused,
	}
	for repoID, state := range want {
		if first[repoID].State != state {
			t.Errorf("%s should report what its round did, got %+v", repoID, first[repoID])
		}
	}
	if offline := first["example.test/acme/offline"]; offline.Reason == "" || !offline.Next.After(now) {
		t.Errorf("a failure should carry its reason and when it is retried, got %+v", offline)
	}
	if c.git.Clones != 1 {
		t.Error("the uncloned repo should be cloned once")
	}

	second := c.sync(t, &command.RepoSyncCmd{RepoIDs: []model.RepoID{"example.test/acme/quiet"}})
	if quiet := second["example.test/acme/quiet"]; quiet.State != model.RepoSkipped || !quiet.Next.After(now) {
		t.Errorf("a repo within its fetch cooldown should be skipped with when it is due, got %+v", quiet)
	}
	if c.repos["example.test/acme/quiet"].Count("fetch") != 1 {
		t.Error("a skipped repo should not be fetched")
	}
}

// With no repo named, every connected repo is synced.
func TestRepoSyncDefaultsToEveryConnectedRepo(t *testing.T) {
	c := newConnections(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC), map[model.RepoID]*synctest.Repo{
		"example.test/acme/one": {Upstream: "origin/main"},
		"example.test/acme/two": {Upstream: "origin/main"},
	})
	outcomes := c.sync(t, &command.RepoSyncCmd{})
	if len(outcomes) != 3 {
		t.Errorf("every connected repo should be synced, got %+v", outcomes)
	}
}

func (c *connections) fresh(t *testing.T, repoID model.RepoID) (changed bool, phases []model.Phase) {
	t.Helper()
	changed, err := c.handler.EnsureReposFresh(t.Context(), command.EnsureReposFreshCmd{
		RepoIDs: []model.RepoID{repoID}, OnPhase: func(phase model.Phase) { phases = append(phases, phase) },
	})
	if err != nil {
		t.Fatalf("a refresh should never fail on a deferral: %v", err)
	}
	return changed, phases
}

// A refresh for a read renews the clone's lease and runs the round only when
// the lease starts past the fetch cooldown; otherwise it leaves the clone as
// it stands, reporting no phase for it.
func TestEnsureReposFreshRunsARoundOnALeaseStart(t *testing.T) {
	const repoID = model.RepoID("example.test/acme/other")
	for _, tc := range []struct {
		name   string
		before func(t *testing.T, c *connections)
		round  bool
	}{
		{name: "a lease start past the cooldown", round: true},
		{name: "a renewal while the lease holds", before: func(t *testing.T, c *connections) {
			c.fresh(t, repoID)
			*c.now = c.now.Add(9 * time.Minute)
		}},
		{name: "a lease start inside the cooldown", before: func(t *testing.T, c *connections) {
			c.sync(t, &command.RepoSyncCmd{RepoIDs: []model.RepoID{repoID}})
			*c.now = c.now.Add(time.Minute)
		}},
		{name: "a lease start after the pause", round: true, before: func(t *testing.T, c *connections) {
			c.fresh(t, repoID)
			*c.now = c.now.Add(11 * time.Minute)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newConnections(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC), map[model.RepoID]*synctest.Repo{repoID: {Upstream: "origin/main"}})
			if tc.before != nil {
				tc.before(t, c)
			}
			before := c.round.count()
			changed, phases := c.fresh(t, repoID)
			if ran := c.round.count() - before; (ran == 1) != tc.round || changed != tc.round || (len(phases) > 0) != tc.round {
				t.Errorf("want a round %v, got %d rounds, changed %v, phases %v", tc.round, ran, changed, phases)
			}
		})
	}
}

// A local-path remote, which the git gate admits every time, runs its round
// only when its lease starts.
func TestEnsureReposFreshRunsALocalRemoteOnALeaseStart(t *testing.T) {
	const repoID = model.RepoID("example.test/acme/local")
	c := newConnections(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC), map[model.RepoID]*synctest.Repo{repoID: {Upstream: "origin/main"}})
	c.repos[repoID].URL = "/srv/graphs/local.git"
	c.fresh(t, repoID)
	c.fresh(t, repoID)
	if c.round.count() != 1 {
		t.Fatalf("a renewal while the lease holds should run no round, ran %d", c.round.count())
	}
	*c.now = c.now.Add(11 * time.Minute)
	c.fresh(t, repoID)
	if c.round.count() != 2 {
		t.Error("a lease start after the pause should run the round again")
	}
}

// The timer of a running server fetches the cloned members of the dependency
// closure whose lease holds and whose fetch is due, pushes the commits a
// member without a lease holds, and reports nothing for the rest: a member
// without a lease or within its cooldown, one not cloned, one not connected,
// one connected outside the closure.
func TestRepoSyncDueRunsTheTimerRoundOfTheClosureMembers(t *testing.T) {
	const (
		leased  = model.RepoID("example.test/acme/leased")
		idle    = model.RepoID("example.test/acme/idle")
		ahead   = model.RepoID("example.test/acme/ahead")
		outside = model.RepoID("example.test/acme/outside")
	)
	c := newConnections(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC), map[model.RepoID]*synctest.Repo{
		leased:  {Upstream: "origin/main"},
		idle:    {Upstream: "origin/main"},
		ahead:   {Upstream: "origin/main", Ahead: 1, GraphAhead: 1},
		outside: {Upstream: "origin/main"},
	})
	reader := finders.New(finders.Options{
		Config: &model.PerRepoConfig{RepoID: "example.test/acme/home", Dependencies: []model.RepoID{leased, idle, ahead, uncloned, "example.test/acme/unconnected"}},
		Repos:  c.manager.Registry(),
	})
	c.handler = handlers.New(handlers.Options{Reader: reader, Repos: c.manager, CloneRound: c.round})
	c.fresh(t, leased)
	*c.now = c.now.Add(6 * time.Minute)

	first := c.sync(t, &command.RepoSyncCmd{Due: true})
	if len(first) != 2 || first[leased].State != model.RepoSynced || first[ahead].State != model.RepoPushed {
		t.Errorf("the leased member should sync and the member with commits waiting push, got %+v", first)
	}
	if c.repos[ahead].Count("fetch") != 0 || c.repos[idle].Count("fetch") != 0 {
		t.Error("a member without a lease should not fetch")
	}
	if c.git.Clones != 0 || c.repos[outside].Count("fetch") != 0 {
		t.Error("the timer should neither clone a member nor reach a repo outside the closure")
	}
	c.repos[ahead].Ahead, c.repos[ahead].GraphAhead = 0, 0
	second := c.sync(t, &command.RepoSyncCmd{Due: true})
	if len(second) != 0 || c.repos[leased].Count("fetch") != 2 {
		t.Errorf("a member within its cooldown should run no round and report nothing, got %+v", second)
	}
}

// Rounds against one host run at most the gate's concurrency at once.
func TestRepoSyncHoldsTheHostConcurrency(t *testing.T) {
	scripted := map[model.RepoID]*synctest.Repo{}
	for _, name := range []string{"one", "two", "three", "four", "five"} {
		scripted[model.RepoID("example.test/acme/"+name)] = &synctest.Repo{Upstream: "origin/main"}
	}
	c := newConnections(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC), scripted)
	c.round.hold = 50 * time.Millisecond
	var named []model.RepoID
	for repoID := range scripted {
		named = append(named, repoID)
	}
	c.sync(t, &command.RepoSyncCmd{RepoIDs: named})
	if c.round.maxInFlight > model.DefaultGitConcurrency || c.round.maxInFlight < 2 {
		t.Errorf("rounds against one host should run in parallel up to its concurrency, got %d at once", c.round.maxInFlight)
	}
}

// A handler built without the clone round refuses a sync loud.
func TestRepoSyncWithoutTheCloneRound(t *testing.T) {
	c := newConnections(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC), nil)
	handler := handlers.New(handlers.Options{Repos: c.manager})
	err := handler.RepoSync(t.Context(), &command.RepoSyncCmd{OnOutcome: func(model.RepoSyncOutcome) {}})
	if err == nil {
		t.Error("a sync without the clone round should fail")
	}
	_, err = handler.EnsureReposFresh(t.Context(), command.EnsureReposFreshCmd{RepoIDs: []model.RepoID{uncloned}})
	if err == nil {
		t.Error("a refresh without the clone round should fail")
	}
}

// `sdd repo sync` and sync_repos are commanded: inside a cooldown a credit
// pays for the fetch, or for the push of commits waiting.
func TestRepoSyncSpendsCredits(t *testing.T) {
	const repoID = model.RepoID("example.test/acme/graph")
	cases := []struct {
		name  string
		repo  *synctest.Repo
		after time.Duration
		state model.RepoSyncState
		op    string
	}{
		{name: "fetch", repo: &synctest.Repo{Upstream: "origin/main"}, after: 90 * time.Second, state: model.RepoSynced, op: "fetch"},
		{name: "push of commits waiting", repo: &synctest.Repo{Upstream: "origin/main", Ahead: 1, GraphAhead: 1}, after: 15 * time.Second, state: model.RepoPushed, op: "push"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newConnections(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC), map[model.RepoID]*synctest.Repo{repoID: tc.repo})
			c.sync(t, &command.RepoSyncCmd{RepoIDs: []model.RepoID{repoID}})
			// The fetch slices left unused meanwhile earn credits.
			*c.now = c.now.Add(20 * time.Minute)
			c.sync(t, &command.RepoSyncCmd{RepoIDs: []model.RepoID{repoID}})

			*c.now = c.now.Add(tc.after)
			outcome := c.sync(t, &command.RepoSyncCmd{RepoIDs: []model.RepoID{repoID}})[repoID]
			if outcome.State != tc.state || tc.repo.Count(tc.op) != 3 {
				t.Errorf("a commanded sync inside the cooldown should spend a credit, got %+v after %d runs", outcome, tc.repo.Count(tc.op))
			}
		})
	}
}
