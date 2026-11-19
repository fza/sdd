package proctest_test

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/local"
)

// graphEntries counts the entry files under the world's graph.
func graphEntries(t *testing.T, graphDir string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(graphDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(graphDir, path)
		if err != nil {
			return err
		}
		if _, err := model.RelPathToID(filepath.ToSlash(rel)); err == nil {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}

// A publish failing after its entry is written leaves the operation pending;
// its retry publishes no second entry and announces the written one once.
func TestCapture_ARetryAfterAFailureBehindTheWriteKeepsOneEntry(t *testing.T) {
	store := &failingRelevanceStore{RelevanceStore: local.NewMemoryRelevanceStore()}
	scripted := &proctest.ScriptedHooks{Configured: []sdd.Hook{{HookConfig: model.HookConfig{Event: "entry.written", Command: "notify"}}}, Scripts: map[string]proctest.HookScript{}}
	world, session := newCaptureWorld(t, "retry-after-write", proctest.WithGroundingEnforced(), proctest.WithRelevance(store), proctest.WithHooks(scripted))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	proctest.RequireStep(t, session.Report(t, instance, captureDraft()), "playback")
	before := graphEntries(t, world.GraphDir)
	store.armed = true
	failed, err := session.AnswerErr(t, instance, "playback", "confirm", nil, "write it")
	if err != nil {
		t.Fatal(err)
	}
	pending := failed.PendingOperation
	if pending == nil || store.armed {
		t.Fatalf("a failure behind the write should leave the publish pending, served %+v", failed)
	}
	entryID := pending.Values["entryId"]
	proctest.LoadEntry(t, world.GraphDir, entryID)
	serve, err = session.WF.Advance(t.Context(), world.Identity, sdd.WorkflowAdvanceRequest{Instance: instance, RetryRef: pending.RetryRef})
	if err != nil {
		t.Fatal(err)
	}
	proctest.RequireStep(t, serve, "verifySummary")
	if got := graphEntries(t, world.GraphDir) - before; got != 1 {
		t.Fatalf("the retry should publish no second entry, the graph gained %d", got)
	}
	written := scripted.RunsAt("entry.written")
	if len(written) != 1 || written[0].Payload["entry"] != entryID {
		t.Fatalf("entry.written should fire once naming the written entry, got %+v", written)
	}
	serve = session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	if serve.Produced["entryId"] != entryID {
		t.Fatalf("the capture should produce the entry the first attempt wrote, got %v", serve.Produced["entryId"])
	}
}
