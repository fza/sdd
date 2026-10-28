package presenters_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/query"
)

func TestRenderSyncBlockedEscapesRepositoryText(t *testing.T) {
	var out bytes.Buffer
	presenters.RenderSyncBlocked(&out, &model.SyncBlocked{
		Kind: model.SyncBlockedConflict, Since: time.Now(), Upstream: "origin/main",
		Paths: []string{".sdd/graph/\x1b]0;owned\x07a.md"}, Detail: "fatal: \x1b[2J",
	}, time.Now())
	if strings.ContainsAny(out.String(), "\x1b\x07") {
		t.Errorf("paths and git output should print escaped, got %q", out.String())
	}
}

func TestRenderSyncBlockedLeavesAnOwnedFixToItsOwner(t *testing.T) {
	now := time.Now()
	block := model.SyncBlocked{Kind: model.SyncBlockedDirty, Since: now}
	for _, tc := range []struct {
		name      string
		block     model.SyncBlocked
		wantSteps bool
	}{
		{name: "unowned", block: block, wantSteps: true},
		{name: "owned", block: block.Claim("s_1", now), wantSteps: false},
		{name: "lease ran out", block: block.Claim("s_1", now.Add(-model.OwnerLease-time.Minute)), wantSteps: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			presenters.RenderSyncBlocked(&out, &tc.block, now)
			if got := strings.Contains(out.String(), tc.block.Steps()); got != tc.wantSteps {
				t.Errorf("the steps should print only while no agent owns the fix, got %q", out.String())
			}
		})
	}
}

func TestRenderInfoJSONNamesOnlyALiveOwner(t *testing.T) {
	now := time.Now()
	block := model.SyncBlocked{Kind: model.SyncBlockedDirty, Since: now}
	for _, tc := range []struct {
		name      string
		block     *model.SyncBlocked
		wantOwner string
	}{
		{name: "sync proceeds", block: nil},
		{name: "unowned", block: &block},
		{name: "owned", block: new(block.Claim("s_1", now)), wantOwner: "s_1"},
		{name: "lease ran out", block: new(block.Claim("s_1", now.Add(-model.OwnerLease-time.Minute)))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := presenters.RenderInfoJSON(&out, &query.InfoResult{}, tc.block, now)
			if err != nil {
				t.Fatal(err)
			}
			var info struct {
				SyncBlocked *struct {
					Kind  string `json:"kind"`
					Owner string `json:"owner"`
				} `json:"sync_blocked"`
			}
			if err := json.Unmarshal(out.Bytes(), &info); err != nil {
				t.Fatal(err)
			}
			if (info.SyncBlocked == nil) != (tc.block == nil) {
				t.Fatalf("sync_blocked should be present exactly while sync is blocked, got %s", out.String())
			}
			if info.SyncBlocked != nil && (info.SyncBlocked.Kind != string(tc.block.Kind) || info.SyncBlocked.Owner != tc.wantOwner) {
				t.Errorf("the owner should be named only while its lease is live, got %s", out.String())
			}
		})
	}
}
