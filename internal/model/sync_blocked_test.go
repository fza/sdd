package model_test

import (
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

func TestSyncBlockedPauses(t *testing.T) {
	kinds := []model.SyncBlockedKind{
		model.SyncBlockedConflict, model.SyncBlockedForeign, model.SyncBlockedOutsideGraph, model.SyncBlockedDirty,
		model.SyncBlockedUnpublished, model.SyncBlockedNoUpstream, model.SyncBlockedFetchFailed, model.SyncBlockedPushFailed,
		model.SyncBlockedGraphMissing,
	}
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			if got := (model.SyncBlocked{Kind: kind}).Pauses(); got != (kind == model.SyncBlockedConflict) {
				t.Error("only a conflict should pause automatic sync")
			}
		})
	}
}

func TestSyncBlockedRenew(t *testing.T) {
	began := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	now := began.Add(time.Hour)
	previous := &model.SyncBlocked{Kind: model.SyncBlockedFetchFailed, Since: began, Owner: "s_1"}
	cases := []struct {
		name      string
		previous  *model.SyncBlocked
		kind      model.SyncBlockedKind
		wantSince time.Time
		wantOwner string
	}{
		{name: "first time", kind: model.SyncBlockedFetchFailed, wantSince: now},
		{name: "same kind again", previous: previous, kind: model.SyncBlockedFetchFailed, wantSince: began, wantOwner: "s_1"},
		{name: "another kind", previous: previous, kind: model.SyncBlockedDirty, wantSince: now},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := model.SyncBlocked{Kind: tc.kind, Since: now}.Renew(tc.previous)
			if !got.Since.Equal(tc.wantSince) || got.Owner != tc.wantOwner {
				t.Errorf("a block found again should keep its start and owner, a new one starts afresh, got %v %q", got.Since, got.Owner)
			}
		})
	}
}

func TestSyncBlockedClaim(t *testing.T) {
	unowned := model.SyncBlocked{Kind: model.SyncBlockedDirty}
	if got := unowned.Claim("s_1").Owner; got != "s_1" {
		t.Errorf("the first session to claim should own the block, got %q", got)
	}
	if got := unowned.Claim("s_1").Claim("s_2").Owner; got != "s_1" {
		t.Errorf("a later session should not take the block over, got %q", got)
	}
}

func TestSyncBlockedNoticeFor(t *testing.T) {
	block := model.SyncBlocked{
		Kind: model.SyncBlockedConflict, Since: time.Now(), Upstream: "origin/main",
		Source: "0123456789abcdef", Paths: []string{".sdd/graph/a.md"}, Owner: "s_1",
	}
	owner := block.NoticeFor("s_1")
	other := block.NoticeFor("s_2")
	if !strings.HasPrefix(owner, block.Banner()) || !strings.HasPrefix(other, block.Banner()) {
		t.Error("every session's notice should open with the banner")
	}
	if !strings.Contains(owner, block.Steps()) {
		t.Error("the owner's notice should carry the fix steps")
	}
	if strings.Contains(other, "git merge") {
		t.Errorf("another session's notice should leave the fix to the owner, got %q", other)
	}
}

func TestSyncBlockedConflictSteps(t *testing.T) {
	block := model.SyncBlocked{Kind: model.SyncBlockedConflict, Upstream: "origin/main", Source: "0123456789abcdef"}
	steps := block.Steps()
	for _, want := range []string{"git merge origin/main", model.GraphMergeMessage("origin/main"), "0123456789ab", "ask how to proceed"} {
		if !strings.Contains(steps, want) {
			t.Errorf("the conflict steps should name the merge, its message, the commit to wait for and the way out, missing %q in %q", want, steps)
		}
	}
}

func TestHeldVerdictBlocks(t *testing.T) {
	pulls := map[model.PullVerdict]bool{
		model.PullForeign: true, model.PullOutsideGraph: true, model.PullDirty: true, model.PullNoUpstream: true,
		model.PullReady: false, model.PullNothing: false, model.PullOff: false,
	}
	for verdict, blocks := range pulls {
		if _, got := model.HeldPullBlock(verdict); got != blocks {
			t.Errorf("pull verdict %q should block only when it holds commits back", verdict)
		}
	}
	pushes := map[model.PushVerdict]bool{
		model.PushUnpublished: true, model.PushNoUpstream: true,
		model.PushReady: false, model.PushNothing: false, model.PushOff: false,
	}
	for verdict, blocks := range pushes {
		if _, got := model.HeldPushBlock(verdict); got != blocks {
			t.Errorf("push verdict %q should block only when it holds commits back", verdict)
		}
	}
}
