package finders_test

import (
	"context"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

func TestSyncStatusWithoutGitSurfaceIsSkipped(t *testing.T) {
	got, err := finders.New(finders.Options{}).SyncStatus(context.Background(), query.SyncStatusQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.SyncStateSkipped {
		t.Errorf("no git surface should skip the check, got %q", got.State)
	}
}

func TestSyncStatus(t *testing.T) {
	conflicts := []string{".sdd/graph/2026/04/23-143213-d-tac-hsu.md"}
	cases := []struct {
		name string
		repo *synctest.Repo
		want model.SyncStatus
	}{
		{name: "not a repository", repo: &synctest.Repo{NoRepo: true}, want: model.SyncStatus{State: model.SyncStateNoRepo}},
		{name: "no remote", repo: &synctest.Repo{NoRemote: true}, want: model.SyncStatus{State: model.SyncStateNoRemote}},
		{name: "no upstream", repo: &synctest.Repo{}, want: model.SyncStatus{State: model.SyncStateNoUpstream}},
		{name: "up to date", repo: &synctest.Repo{Upstream: "origin/main"}, want: model.SyncStatus{State: model.SyncStateUpToDate}},
		{name: "behind", repo: &synctest.Repo{Upstream: "origin/main", GraphBehind: 3}, want: model.SyncStatus{State: model.SyncStateFastForward, RemoteAhead: 3}},
		{name: "ahead", repo: &synctest.Repo{Upstream: "origin/main", GraphAhead: 2}, want: model.SyncStatus{State: model.SyncStateLocalAhead, LocalAhead: 2}},
		{name: "diverged, merging cleanly", repo: &synctest.Repo{Upstream: "origin/main", GraphBehind: 2, GraphAhead: 1}, want: model.SyncStatus{State: model.SyncStateCleanRebase, RemoteAhead: 2, LocalAhead: 1}},
		{
			name: "diverged, conflicting", repo: &synctest.Repo{Upstream: "origin/main", GraphBehind: 2, GraphAhead: 1, Conflicts: conflicts},
			want: model.SyncStatus{State: model.SyncStateConflictPredicted, RemoteAhead: 2, LocalAhead: 1, ConflictPaths: conflicts},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := tc.repo
			repo.Forbid = []string{"fetch", "push", "merge"}
			got, err := finders.New(finders.Options{GitSyncer: repo}).SyncStatus(context.Background(), query.SyncStatusQuery{})
			if err != nil {
				t.Fatal(err)
			}
			if got.State != tc.want.State || got.RemoteAhead != tc.want.RemoteAhead || got.LocalAhead != tc.want.LocalAhead || !slices.Equal(got.ConflictPaths, tc.want.ConflictPaths) {
				t.Errorf("the status should follow the refs as last fetched, got %+v", got)
			}
			if repo.Called("fetch") {
				t.Error("the finder should never fetch")
			}
		})
	}
}
