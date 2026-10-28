package handlers_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

type syncFixture struct {
	repo    *synctest.Repo
	lock    *synctest.Lock
	record  *synctest.Record
	handler *handlers.Handler
	sddDir  string
	now     time.Time
}

func newSyncFixture(t *testing.T, repo *synctest.Repo) *syncFixture {
	t.Helper()
	f := &syncFixture{repo: repo, lock: &synctest.Lock{}, record: &synctest.Record{}, sddDir: t.TempDir(), now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	f.handler = handlers.New(handlers.Options{
		GraphDir: "graph", GraphFetcher: repo, GraphPuller: repo, Pusher: repo, SyncLock: f.lock, SyncRecord: f.record,
		SyncMode: model.SyncModeAuto, Now: func() time.Time { return f.now },
	})
	return f
}

// collect returns a context reporting blocked states into the returned slice.
func collect(ctx context.Context) (context.Context, *[]model.SyncBlocked) {
	var reported []model.SyncBlocked
	return handlers.WithSyncReport(ctx, func(block model.SyncBlocked) { reported = append(reported, block) }), &reported
}

func (f *syncFixture) round(t *testing.T, ctx context.Context, cmd command.SyncRoundCmd) {
	t.Helper()
	cmd.SDDDir = f.sddDir
	err := f.handler.SyncRound(ctx, &cmd)
	if err != nil {
		t.Fatal(err)
	}
}

func TestFetchGraph(t *testing.T) {
	cases := []struct {
		name   string
		repo   *synctest.Repo
		recent bool
		want   model.SyncState
		fetch  bool
	}{
		{name: "fetches the upstream", repo: &synctest.Repo{Upstream: "origin/main"}, want: model.SyncStateFetched, fetch: true},
		{name: "within the cooldown", repo: &synctest.Repo{Upstream: "origin/main"}, recent: true, want: model.SyncStateSkipped},
		{name: "not a repository", repo: &synctest.Repo{NoRepo: true}, want: model.SyncStateNoRepo},
		{name: "no remote", repo: &synctest.Repo{NoRemote: true}, want: model.SyncStateNoRemote},
		{name: "no upstream", repo: &synctest.Repo{}, want: model.SyncStateNoUpstream},
		{name: "failing fetch", repo: &synctest.Repo{Upstream: "origin/main", FetchErr: errors.New("could not resolve host")}, want: model.SyncStateFetchFailed, fetch: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sddDir := t.TempDir()
			if tc.recent {
				if err := meta.TouchLastFetch(sddDir); err != nil {
					t.Fatal(err)
				}
			}
			handler := handlers.New(handlers.Options{GraphFetcher: tc.repo})
			var got model.SyncState
			var reason string
			err := handler.FetchGraph(context.Background(), &command.FetchGraphCmd{SDDDir: sddDir, Cooldown: time.Hour, OnFetched: func(state model.SyncState, why string) {
				got, reason = state, why
			}})
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want || tc.repo.Called("fetch") != tc.fetch {
				t.Errorf("the fetch should run only past the cooldown with an upstream, got %q", got)
			}
			if got == model.SyncStateFetchFailed && reason == "" {
				t.Error("a failing fetch should carry its reason")
			}
			last, err := meta.ReadLastFetch(sddDir)
			if err != nil {
				t.Fatal(err)
			}
			if stamped := !last.IsZero(); stamped != (tc.want != model.SyncStateNoRepo) {
				t.Error("every attempt in a repository should stamp the last fetch")
			}
		})
	}
}

func TestSyncRoundRecordsWhatBlocks(t *testing.T) {
	cases := []struct {
		name string
		repo *synctest.Repo
		want model.SyncBlockedKind
	}{
		{name: "no remote", repo: &synctest.Repo{NoRemote: true}, want: model.SyncBlockedNoUpstream},
		{name: "no upstream", repo: &synctest.Repo{}, want: model.SyncBlockedNoUpstream},
		{name: "failing fetch", repo: &synctest.Repo{Upstream: "origin/main", FetchErr: errors.New("offline")}, want: model.SyncBlockedFetchFailed},
		{name: "foreign commit incoming", repo: &synctest.Repo{Upstream: "origin/main", Behind: 2, GraphBehind: 1}, want: model.SyncBlockedForeign},
		{name: "incoming change outside the graph", repo: &synctest.Repo{Upstream: "origin/main", Behind: 1, GraphBehind: 1, Changed: []string{"main.go"}}, want: model.SyncBlockedOutsideGraph},
		{name: "dirty tree", repo: &synctest.Repo{Upstream: "origin/main", Behind: 1, GraphBehind: 1, Changed: []string{"graph/a.md"}, Dirty: true}, want: model.SyncBlockedDirty},
		{name: "foreign commit ahead", repo: &synctest.Repo{Upstream: "origin/main", Ahead: 2, GraphAhead: 1}, want: model.SyncBlockedUnpublished},
		{name: "conflicting merge", repo: &synctest.Repo{Upstream: "origin/main", Behind: 1, GraphBehind: 1, Changed: []string{"graph/a.md"}, Conflicts: []string{"graph/a.md"}}, want: model.SyncBlockedConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSyncFixture(t, tc.repo)
			ctx, reported := collect(context.Background())
			f.round(t, ctx, command.SyncRoundCmd{})
			if f.record.Current == nil || f.record.Current.Kind != tc.want {
				t.Fatalf("the round should record what blocks it, got %+v", f.record.Current)
			}
			if !f.record.Current.Since.Equal(f.now) {
				t.Error("the record should carry when the block began")
			}
			if len(*reported) != 1 || (*reported)[0].Kind != tc.want {
				t.Errorf("the round should report the block once, got %+v", *reported)
			}
			if tc.repo.Called("merge") && tc.want != model.SyncBlockedUnpublished {
				t.Error("a blocked pull should merge nothing")
			}
		})
	}
}

func TestSyncRoundRecordsTheConflictCommits(t *testing.T) {
	repo := &synctest.Repo{
		Upstream: "origin/main", Behind: 1, GraphBehind: 1, Changed: []string{"graph/a.md"}, Conflicts: []string{"graph/a.md"},
		Commits: map[string]string{"origin/main": "upstream-commit", "HEAD": "local-commit"},
	}
	f := newSyncFixture(t, repo)
	f.round(t, context.Background(), command.SyncRoundCmd{})
	got := f.record.Current
	if got == nil || got.Source != "upstream-commit" || got.Destination != "local-commit" || got.Upstream != "origin/main" || len(got.Paths) != 1 {
		t.Errorf("the conflict record should name upstream, both commits and the paths, got %+v", got)
	}
}

func TestSyncRoundClearsTheRecordOnceNothingBlocks(t *testing.T) {
	repo := &synctest.Repo{Upstream: "origin/main", Behind: 1, GraphBehind: 1, Changed: []string{"graph/a.md"}, Ahead: 1, GraphAhead: 1}
	f := newSyncFixture(t, repo)
	f.record.Current = &model.SyncBlocked{Kind: model.SyncBlockedFetchFailed, Since: f.now.Add(-time.Hour)}
	ctx, reported := collect(context.Background())
	f.round(t, ctx, command.SyncRoundCmd{})
	if f.record.Current != nil || len(*reported) != 0 {
		t.Errorf("a round that merges and pushes should remove the record and report nothing, got %+v", f.record.Current)
	}
	if !repo.Called("merge") || !repo.Called("push") {
		t.Error("the round should merge upstream and push local graph commits")
	}
}

func TestSyncRoundKeepsWhenTheBlockBegan(t *testing.T) {
	f := newSyncFixture(t, &synctest.Repo{Upstream: "origin/main", FetchErr: errors.New("offline")})
	began := f.now
	f.round(t, context.Background(), command.SyncRoundCmd{})
	f.now = f.now.Add(time.Hour)
	f.round(t, context.Background(), command.SyncRoundCmd{})
	if !f.record.Current.Since.Equal(began) {
		t.Errorf("a block found again should keep its start, got %v", f.record.Current.Since)
	}
}

func TestSyncRoundSkipsAHeldLock(t *testing.T) {
	repo := &synctest.Repo{Upstream: "origin/main", Forbid: []string{"fetch", "merge", "push"}}
	f := newSyncFixture(t, repo)
	f.lock.Held = true
	f.record.Current = &model.SyncBlocked{Kind: model.SyncBlockedDirty, Since: f.now}
	skipped := false
	ctx, reported := collect(context.Background())
	f.round(t, ctx, command.SyncRoundCmd{OnSkipped: func() { skipped = true }})
	if !skipped || repo.Called("fetch") {
		t.Error("a round finding the lock held should skip without fetching")
	}
	if len(*reported) != 1 {
		t.Errorf("a skipped round should still report the recorded block, got %+v", *reported)
	}
}

func TestSyncRoundPausesOnARecordedConflict(t *testing.T) {
	conflict := func(now time.Time) *model.SyncBlocked {
		return &model.SyncBlocked{Kind: model.SyncBlockedConflict, Since: now, Upstream: "origin/main", Source: "upstream-commit"}
	}
	t.Run("unresolved", func(t *testing.T) {
		repo := &synctest.Repo{Upstream: "origin/main", Forbid: []string{"fetch", "merge", "push"}}
		f := newSyncFixture(t, repo)
		f.record.Current = conflict(f.now)
		ctx, reported := collect(context.Background())
		f.round(t, ctx, command.SyncRoundCmd{})
		if repo.Called("fetch") || repo.Called("merge") || repo.Called("push") {
			t.Error("a recorded conflict should pause fetch, merge and push")
		}
		if f.record.Current == nil || len(*reported) != 1 {
			t.Error("the paused round should keep and report the conflict")
		}
	})
	t.Run("merged by hand", func(t *testing.T) {
		repo := &synctest.Repo{Upstream: "origin/main", Contained: map[string]bool{"upstream-commit": true}}
		f := newSyncFixture(t, repo)
		f.record.Current = conflict(f.now)
		f.round(t, context.Background(), command.SyncRoundCmd{})
		if f.record.Current != nil || !repo.Called("fetch") {
			t.Error("once the source commit is in the branch the record should go and sync resume")
		}
	})
	t.Run("explicit round", func(t *testing.T) {
		repo := &synctest.Repo{Upstream: "origin/main"}
		f := newSyncFixture(t, repo)
		f.record.Current = conflict(f.now)
		f.round(t, context.Background(), command.SyncRoundCmd{Explicit: true})
		if !repo.Called("fetch") {
			t.Error("an explicit round should run while paused")
		}
	})
}

func TestSyncRoundIsOffUnderManualSync(t *testing.T) {
	repo := &synctest.Repo{Upstream: "origin/main"}
	handler := handlers.New(handlers.Options{GraphFetcher: repo, GraphPuller: repo, Pusher: repo})
	err := handler.SyncRound(context.Background(), &command.SyncRoundCmd{SDDDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.Calls) != 0 {
		t.Errorf("manual sync should run no round, got %v", repo.Calls)
	}
}

func TestSyncRoundReportsAMissingGraphRepository(t *testing.T) {
	handler := handlers.New(handlers.Options{GraphDir: "../missing", SyncMode: model.SyncModeAuto})
	ctx, reported := collect(context.Background())
	err := handler.SyncRound(ctx, &command.SyncRoundCmd{SDDDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(*reported) != 1 || (*reported)[0].Kind != model.SyncBlockedGraphMissing || !strings.Contains((*reported)[0].Banner(), "../missing") {
		t.Errorf("a round without a graph repository should report it missing, naming the directory, got %+v", *reported)
	}
}

func TestPublishGraph(t *testing.T) {
	t.Run("paused by a conflict", func(t *testing.T) {
		repo := &synctest.Repo{Upstream: "origin/main", Ahead: 1, GraphAhead: 1, Forbid: []string{"fetch", "merge", "push"}}
		f := newSyncFixture(t, repo)
		f.record.Current = &model.SyncBlocked{Kind: model.SyncBlockedConflict, Since: f.now, Source: "upstream-commit"}
		err := f.handler.PublishGraph(context.Background())
		if err != nil {
			t.Fatalf("a paused publish should leave the commit local and succeed: %v", err)
		}
		if repo.Called("push") || f.lock.Taken != 1 {
			t.Error("the publish should wait for the lock and push nothing while paused")
		}
	})
	t.Run("push rejected, retry still failing", func(t *testing.T) {
		repo := &synctest.Repo{Upstream: "origin/main", Ahead: 1, GraphAhead: 1, PushErr: errors.New("rejected")}
		f := newSyncFixture(t, repo)
		err := f.handler.PublishGraph(context.Background())
		if err != nil {
			t.Fatalf("a failing push should be recorded, not fail the command: %v", err)
		}
		if f.record.Current == nil || f.record.Current.Kind != model.SyncBlockedPushFailed {
			t.Errorf("the failing push should be recorded, got %+v", f.record.Current)
		}
	})
	t.Run("push rejected, upstream conflicting", func(t *testing.T) {
		repo := &synctest.Repo{
			Upstream: "origin/main", Ahead: 1, GraphAhead: 1, PushErr: errors.New("rejected"),
			Behind: 1, GraphBehind: 1, Changed: []string{"graph/a.md"}, Conflicts: []string{"graph/a.md"},
		}
		f := newSyncFixture(t, repo)
		err := f.handler.PublishGraph(context.Background())
		if err != nil {
			t.Fatalf("a conflict found on the push retry should be recorded, not fail the command: %v", err)
		}
		if f.record.Current == nil || f.record.Current.Kind != model.SyncBlockedConflict {
			t.Errorf("the conflict should be recorded, got %+v", f.record.Current)
		}
	})
	t.Run("pushed", func(t *testing.T) {
		repo := &synctest.Repo{Upstream: "origin/main", Ahead: 1, GraphAhead: 1}
		f := newSyncFixture(t, repo)
		f.record.Current = &model.SyncBlocked{Kind: model.SyncBlockedUnpublished, Since: f.now}
		err := f.handler.PublishGraph(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if f.record.Current != nil {
			t.Error("a push that lands should clear a push block")
		}
	})
	t.Run("pushed with a pull block left", func(t *testing.T) {
		repo := &synctest.Repo{Upstream: "origin/main", Ahead: 1, GraphAhead: 1}
		f := newSyncFixture(t, repo)
		f.record.Current = &model.SyncBlocked{Kind: model.SyncBlockedForeign, Since: f.now}
		err := f.handler.PublishGraph(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if f.record.Current == nil {
			t.Error("a push should leave a block it does not resolve")
		}
	})
}

func TestSyncReportDropsRepeatsWithinOneInvocation(t *testing.T) {
	repo := &synctest.Repo{Upstream: "origin/main", Ahead: 2, GraphAhead: 1}
	f := newSyncFixture(t, repo)
	ctx, reported := collect(context.Background())
	f.round(t, ctx, command.SyncRoundCmd{})
	err := f.handler.PublishGraph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(*reported) != 1 {
		t.Errorf("one invocation should report a held push once, got %d", len(*reported))
	}
	next, again := collect(context.Background())
	err = f.handler.PublishGraph(next)
	if err != nil {
		t.Fatal(err)
	}
	if len(*again) != 1 {
		t.Errorf("the next invocation should report the push still held, got %d", len(*again))
	}
}

func TestClaimSyncNotice(t *testing.T) {
	record := &synctest.Record{Current: &model.SyncBlocked{Kind: model.SyncBlockedDirty, Since: time.Now()}}
	handler := handlers.New(handlers.Options{SyncRecord: record})
	claim := func(session string) string {
		var notice string
		err := handler.ClaimSyncNotice(context.Background(), &command.ClaimSyncNoticeCmd{Session: session, OnNotice: func(text string) { notice = text }})
		if err != nil {
			t.Fatal(err)
		}
		return notice
	}
	first := claim("s_1")
	other := claim("s_2")
	again := claim("s_1")
	steps := record.Current.Steps()
	if !strings.Contains(first, steps) || !strings.Contains(again, steps) {
		t.Error("the first session to call should own the block and get the steps on every call")
	}
	if strings.Contains(other, steps) || !strings.HasPrefix(other, record.Current.Banner()) {
		t.Errorf("another session should get the banner alone, got %q", other)
	}
	record.Current = nil
	if claim("s_1") != "" {
		t.Error("no notice should be served once nothing blocks")
	}
}
