package handlers_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/repos/repostest"
)

const (
	furtherRepoID = model.RepoID("example.test/acme/further")
	furtherEntry  = "20261101-100000-s-tac-fur"
)

// further is a handler over one connected repo beyond the direct
// dependencies, its clone backed by a scripted repository whose fetch can
// land entries in the clone.
type further struct {
	handler *handlers.Handler
	round   *repoRound
	repo    *synctest.Repo
	clone   repos.Clone
	now     *time.Time
}

func newFurther(t *testing.T, readWait time.Duration, cloned bool) *further {
	t.Helper()
	dir := t.TempDir()
	loc := repos.Locations{ConfigPath: filepath.Join(dir, "config.yaml"), CacheRoot: filepath.Join(dir, "cache"), DataRoot: filepath.Join(dir, "data"), StateRoot: filepath.Join(dir, "state")}
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	url := "https://" + string(furtherRepoID) + ".git"
	limits := model.DefaultGitLimits()
	limits.ReadWait = readWait
	gate := gitgate.New(gitgate.Options{
		StateDir: loc.StateRoot, Now: func() time.Time { return now }, Limits: func(string) gitgate.Limits { return limits },
		Clones: func(context.Context) ([]gitgate.Target, error) {
			target, err := gitgate.TargetFromURL(url)
			return []gitgate.Target{target}, err
		},
	})
	reg := repos.NewRegistry(loc)
	cloneDir, err := reg.CloneDir(furtherRepoID)
	if err != nil {
		t.Fatal(err)
	}
	repo := &synctest.Repo{Upstream: "origin/main", URL: url}
	round := &repoRound{gate: gate, repos: map[string]*synctest.Repo{cloneDir: repo}}
	git := &synctest.CloneGit{Remotes: map[string]string{cloneDir: url}, Remote: url, Files: map[string]string{".git/HEAD": "c1\n"}}
	cfg := &repos.GlobalConfig{}
	err = cfg.AddRepo(repos.ConnectedRepo{RepoID: furtherRepoID, CloneURL: url})
	if err != nil {
		t.Fatal(err)
	}
	repostest.WriteConfig(t, loc.ConfigPath, cfg)
	f := &further{round: round, repo: repo, clone: repos.Clone{RepoID: furtherRepoID, Dir: cloneDir}, now: &now}
	if cloned {
		f.write(t, ".git/HEAD", "c1\n")
	}
	manager := repos.NewManager(reg, repos.NewGraphClones(loc, git, gate))
	f.handler = handlers.New(handlers.Options{Repos: manager, CloneRound: round, Now: func() time.Time { return now }})
	return f
}

func (f *further) write(t *testing.T, rel, content string) {
	t.Helper()
	path := filepath.Join(f.clone.Dir, filepath.FromSlash(rel))
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

// landOnFetch makes the next fetch bring the entry into the clone.
func (f *further) landOnFetch(t *testing.T, id string) {
	t.Helper()
	rel, err := model.IDToRelPath(id)
	if err != nil {
		t.Fatal(err)
	}
	f.repo.OnFetch = func() {
		f.write(t, ".sdd/graph/"+rel, "---\ntype: signal\nkind: gap\nlayer: tactical\n---\n\nLanded.\n")
	}
}

// resolve runs the command for id and reports what it said.
func (f *further) resolve(t *testing.T, id string, fetchedBefore bool) (fetched bool, unavailable *model.UnavailableRef) {
	t.Helper()
	err := f.handler.ResolveFurtherRef(t.Context(), &command.ResolveFurtherRefCmd{
		RepoID: furtherRepoID, EntryID: id, Fetched: fetchedBefore,
		OnFetched:     func() { fetched = true },
		OnUnavailable: func(u model.UnavailableRef) { unavailable = &u },
	})
	if err != nil {
		t.Fatal(err)
	}
	return fetched, unavailable
}

func TestResolveFurtherRefReadsTheCloneWithoutFetching(t *testing.T) {
	f := newFurther(t, time.Second, true)
	rel, err := model.IDToRelPath(furtherEntry)
	if err != nil {
		t.Fatal(err)
	}
	f.write(t, ".sdd/graph/"+rel, "---\ntype: signal\nkind: gap\nlayer: tactical\n---\n\nPresent.\n")
	fetched, unavailable := f.resolve(t, furtherEntry, false)
	if fetched || unavailable != nil || f.round.count() != 0 {
		t.Errorf("an entry the clone holds should resolve without a fetch, fetched %v unavailable %+v rounds %d", fetched, unavailable, f.round.count())
	}
}

func TestResolveFurtherRefNotConnected(t *testing.T) {
	f := newFurther(t, time.Second, true)
	var unavailable *model.UnavailableRef
	err := f.handler.ResolveFurtherRef(t.Context(), &command.ResolveFurtherRefCmd{
		RepoID: "example.test/acme/elsewhere", EntryID: furtherEntry,
		OnUnavailable: func(u model.UnavailableRef) { unavailable = &u },
	})
	if err != nil {
		t.Fatal(err)
	}
	if unavailable == nil || unavailable.Kind != model.RefNotConnected || unavailable.At.IsZero() || f.round.count() != 0 {
		t.Errorf("a repo not connected should be reported without a fetch and with the time seen, got %+v", unavailable)
	}
}

func TestResolveFurtherRefFetchesOnAMiss(t *testing.T) {
	t.Run("into an existing clone", func(t *testing.T) {
		f := newFurther(t, 5*time.Second, true)
		f.landOnFetch(t, furtherEntry)
		fetched, unavailable := f.resolve(t, furtherEntry, false)
		if !fetched || unavailable != nil || f.repo.Count("fetch") != 1 {
			t.Errorf("a miss should fetch once and resolve, fetched %v unavailable %+v fetches %d", fetched, unavailable, f.repo.Count("fetch"))
		}
	})
	t.Run("cloning a connected repo first", func(t *testing.T) {
		f := newFurther(t, 5*time.Second, false)
		_, unavailable := f.resolve(t, furtherEntry, false)
		if !f.clone.Cloned() || unavailable == nil || unavailable.Kind != model.RefMissingAfterFetch {
			t.Errorf("a connected repo without a clone should be cloned, then its miss recorded, got %+v", unavailable)
		}
	})
	t.Run("waiting at most read_wait", func(t *testing.T) {
		f := newFurther(t, time.Second, true)
		f.round.hold = 2 * time.Second
		fetched, unavailable := f.resolve(t, furtherEntry, false)
		f.handler.Settle()
		if fetched || unavailable == nil || unavailable.Kind != model.RefFetchDeferred {
			t.Errorf("a fetch outlasting read_wait should defer the ref, got %+v", unavailable)
		}
	})
}

func TestResolveFurtherRefRecordsAMissAfterAFetch(t *testing.T) {
	f := newFurther(t, 5*time.Second, true)
	recorded := *f.now
	_, unavailable := f.resolve(t, furtherEntry, false)
	if unavailable == nil || unavailable.Kind != model.RefMissingAfterFetch || !unavailable.At.Equal(recorded) {
		t.Fatalf("a completed fetch lacking the entry should record the miss, got %+v", unavailable)
	}

	*f.now = recorded.Add(5 * time.Hour)
	_, unavailable = f.resolve(t, furtherEntry, false)
	if f.repo.Count("fetch") != 1 || unavailable == nil || unavailable.Kind != model.RefMissingAfterFetch || !unavailable.At.Equal(recorded) {
		t.Errorf("a recorded miss should hold off the next fetch while the clone stands still, fetches %d, got %+v", f.repo.Count("fetch"), unavailable)
	}

	f.write(t, ".git/HEAD", "c2\n")
	_, _ = f.resolve(t, furtherEntry, false)
	if f.repo.Count("fetch") != 2 {
		t.Errorf("a moved clone commit should allow the next fetch, fetches %d", f.repo.Count("fetch"))
	}

	*f.now = f.now.Add(model.RefMissRetry)
	_, _ = f.resolve(t, furtherEntry, false)
	if f.repo.Count("fetch") != 3 {
		t.Errorf("a day after the miss the next fetch should run, fetches %d", f.repo.Count("fetch"))
	}

	_, unavailable = f.resolve(t, "20261101-110000-s-tac-oth", true)
	if f.repo.Count("fetch") != 3 || unavailable == nil || unavailable.Kind != model.RefMissingAfterFetch {
		t.Errorf("a miss after this read's fetch should record without another fetch, fetches %d, got %+v", f.repo.Count("fetch"), unavailable)
	}
}

func TestResolveFurtherRefSpendsACreditInsideTheCooldown(t *testing.T) {
	t.Run("no credit defers", func(t *testing.T) {
		f := newFurther(t, 5*time.Second, true)
		_, _ = f.resolve(t, furtherEntry, false)
		*f.now = f.now.Add(90 * time.Second)
		_, unavailable := f.resolve(t, "20261101-110000-s-tac-oth", false)
		if f.repo.Count("fetch") != 1 || unavailable == nil || unavailable.Kind != model.RefFetchDeferred || !unavailable.At.After(*f.now) {
			t.Errorf("inside the cooldown without a credit the fetch should defer until it ends, fetches %d, got %+v", f.repo.Count("fetch"), unavailable)
		}
	})
	t.Run("a credit pays", func(t *testing.T) {
		f := newFurther(t, 5*time.Second, true)
		_, _ = f.resolve(t, furtherEntry, false)
		// The fetch slices left unused meanwhile earn credits.
		*f.now = f.now.Add(20 * time.Minute)
		_, _ = f.resolve(t, "20261101-110000-s-tac-two", false)
		*f.now = f.now.Add(90 * time.Second)
		_, unavailable := f.resolve(t, "20261101-120000-s-tac-thr", false)
		if f.repo.Count("fetch") != 3 || unavailable == nil || unavailable.Kind != model.RefMissingAfterFetch {
			t.Errorf("inside the cooldown a credit should pay for the fetch, fetches %d, got %+v", f.repo.Count("fetch"), unavailable)
		}
	})
}
