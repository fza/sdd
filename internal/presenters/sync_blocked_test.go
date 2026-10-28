package presenters_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
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
