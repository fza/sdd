package handlers_test

import (
	"context"
	"testing"
	gosynctest "testing/synctest"
	"time"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/model"
)

func (f *syncFixture) demand(t *testing.T) (rounds int) {
	t.Helper()
	err := f.handler.DemandSync(context.Background(), &command.DemandSyncCmd{OnRound: func() { rounds++ }})
	if err != nil {
		t.Fatal(err)
	}
	return rounds
}

func TestDemandSync(t *testing.T) {
	for _, tc := range []struct {
		name string
		// before runs ahead of the demand under test.
		before func(t *testing.T, f *syncFixture)
		round  bool
	}{
		{name: "a lease start past the fetch cooldown syncs", round: true},
		{name: "a lease start inside the fetch cooldown spends nothing", round: false, before: func(t *testing.T, f *syncFixture) {
			f.fetchedJustNow(t)
			f.now = f.now.Add(time.Minute)
		}},
		{name: "a renewal while the lease holds syncs nothing", round: false, before: func(t *testing.T, f *syncFixture) {
			f.demand(t)
			f.repo.Calls = nil
			f.now = f.now.Add(9 * time.Minute)
		}},
		{name: "a lease start after the pause syncs again", round: true, before: func(t *testing.T, f *syncFixture) {
			f.demand(t)
			f.repo.Calls = nil
			f.now = f.now.Add(11 * time.Minute)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSyncFixture(t, &synctest.Repo{Upstream: "origin/main", URL: hostedURL})
			if tc.before != nil {
				tc.before(t, f)
			}
			rounds := f.demand(t)
			if (rounds == 1) != tc.round || f.repo.Called("fetch") != tc.round {
				t.Errorf("want a round %v, got %d rounds and calls %v", tc.round, rounds, f.repo.Calls)
			}
		})
	}
}

func TestDemandSyncUnderManualSyncRunsNothing(t *testing.T) {
	repo := &synctest.Repo{Upstream: "origin/main", URL: hostedURL}
	handler := handlers.New(handlers.Options{
		GraphFetcher: repo, GraphPuller: repo, Pusher: repo, SyncMode: model.SyncModeManual,
		Gate: synctest.Gate(t.TempDir(), nil, hostedURL),
	})
	err := handler.DemandSync(context.Background(), &command.DemandSyncCmd{})
	if err != nil || len(repo.Calls) != 0 {
		t.Errorf("a graph in the code repository should sync as before, got calls %v, err %v", repo.Calls, err)
	}
}

// slowRepo fetches for hold.
type slowRepo struct {
	*synctest.Repo
	hold time.Duration
}

func (r slowRepo) Fetch(ctx context.Context) error {
	time.Sleep(r.hold)
	return r.Repo.Fetch(ctx)
}

func TestDemandSyncWaitsAtMostTheReadWait(t *testing.T) {
	for _, tc := range []struct {
		name    string
		hold    time.Duration
		waited  time.Duration
		pending bool
	}{
		{name: "a round within the read wait is waited for", hold: 5 * time.Second, waited: 5 * time.Second},
		{name: "a round past the read wait finishes in the background", hold: 40 * time.Second, waited: model.DefaultGitReadWait, pending: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gosynctest.Test(t, func(t *testing.T) {
				repo := &synctest.Repo{Upstream: "origin/main", URL: hostedURL}
				handler := handlers.New(handlers.Options{
					GraphDir: "graph", GraphFetcher: slowRepo{Repo: repo, hold: tc.hold}, GraphPuller: repo, Pusher: repo,
					SyncLock: &synctest.Lock{}, SyncRecord: &synctest.Record{}, SyncMode: model.SyncModeAuto,
					Gate: synctest.Gate(t.TempDir(), nil, hostedURL),
				})
				var pending, settled bool
				start := time.Now()
				err := handler.DemandSync(context.Background(), &command.DemandSyncCmd{
					OnPending: func(fetched time.Time) {
						pending = true
						if !fetched.IsZero() {
							t.Errorf("a graph never fetched should report no fetch time, got %v", fetched)
						}
					},
					OnSettled: func() { settled = true },
				})
				if err != nil {
					t.Fatal(err)
				}
				if waited := time.Since(start); waited != tc.waited {
					t.Errorf("the call should wait %v, waited %v", tc.waited, waited)
				}
				if pending != tc.pending || settled {
					t.Errorf("want pending %v and nothing settled yet, got pending %v, settled %v", tc.pending, pending, settled)
				}
				handler.Settle()
				if !repo.Called("fetch") || settled != tc.pending {
					t.Errorf("the round should finish, settled %v reported only for a pending one; got calls %v, settled %v", tc.pending, repo.Calls, settled)
				}
			})
		})
	}
}
