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

func (r *repoRound) Due(ctx context.Context, dir string) (bool, error) {
	repo, err := r.repo(dir)
	if err != nil {
		return false, err
	}
	url, err := repo.UpstreamURL(ctx)
	if err != nil {
		return false, err
	}
	target, err := gitgate.TargetFromURL(url)
	if err != nil {
		return false, err
	}
	due, _, err := r.gate.Due(target, gitgate.Fetch)
	return due, err
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
}

const uncloned = model.RepoID("example.test/acme/uncloned")

func newConnections(t *testing.T, now time.Time, scripted map[model.RepoID]*synctest.Repo) *connections {
	t.Helper()
	dir := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(dir, "config.yaml"), CacheRoot: filepath.Join(dir, "cache"), DataRoot: filepath.Join(dir, "data")}
	cfg := &repos.GlobalConfig{}
	gate := synctest.Gate(t.TempDir(), func() time.Time { return now })
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
	return &connections{handler: handlers.New(handlers.Options{Repos: manager, CloneRound: round}), manager: manager, round: round, git: git, repos: scripted}
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

// A refresh for a read runs the round only while it is due and leaves a
// clone within its cooldown as it stands, reporting no phase for it.
func TestEnsureReposFreshLeavesADeferredCloneAsItStands(t *testing.T) {
	const repoID = model.RepoID("example.test/acme/other")
	c := newConnections(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC), map[model.RepoID]*synctest.Repo{repoID: {Upstream: "origin/main"}})
	fresh := func() (bool, []model.Phase) {
		var phases []model.Phase
		changed, err := c.handler.EnsureReposFresh(t.Context(), command.EnsureReposFreshCmd{
			RepoIDs: []model.RepoID{repoID}, OnPhase: func(phase model.Phase) { phases = append(phases, phase) },
		})
		if err != nil {
			t.Fatalf("a refresh should never fail on a deferral: %v", err)
		}
		return changed, phases
	}

	changed, _ := fresh()
	if !changed || c.repos[repoID].Count("fetch") != 1 {
		t.Fatal("the first refresh should run the round and fetch")
	}
	changed, phases := fresh()
	if changed || c.round.count() != 1 {
		t.Error("a refresh within the fetch cooldown should run no round")
	}
	if len(phases) != 0 {
		t.Errorf("a refresh not due should report no phase, got %v", phases)
	}
}

// The timer of a running server syncs the cloned members of the dependency
// closure whose round is due, and reports nothing for the rest: a repo not
// due, one not cloned, one not connected, one connected outside the closure.
func TestRepoSyncDueRunsTheDueClosureMembers(t *testing.T) {
	const (
		member  = model.RepoID("example.test/acme/member")
		outside = model.RepoID("example.test/acme/outside")
	)
	c := newConnections(t, time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC), map[model.RepoID]*synctest.Repo{
		member:  {Upstream: "origin/main"},
		outside: {Upstream: "origin/main"},
	})
	reader := finders.New(finders.Options{
		Config: &model.PerRepoConfig{RepoID: "example.test/acme/home", Dependencies: []model.RepoID{member, uncloned, "example.test/acme/unconnected"}},
		Repos:  c.manager.Registry(),
	})
	c.handler = handlers.New(handlers.Options{Reader: reader, Repos: c.manager, CloneRound: c.round})

	first := c.sync(t, &command.RepoSyncCmd{Due: true})
	if len(first) != 1 || first[member].State != model.RepoSynced {
		t.Errorf("only the due cloned member should be synced, got %+v", first)
	}
	if c.git.Clones != 0 || c.repos[outside].Count("fetch") != 0 {
		t.Error("the timer should neither clone a member nor reach a repo outside the closure")
	}
	second := c.sync(t, &command.RepoSyncCmd{Due: true})
	if len(second) != 0 || c.repos[member].Count("fetch") != 1 {
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
