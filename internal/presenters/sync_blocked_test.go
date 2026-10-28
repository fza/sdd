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
	})
	if strings.ContainsAny(out.String(), "\x1b\x07") {
		t.Errorf("paths and git output should print escaped, got %q", out.String())
	}
}
