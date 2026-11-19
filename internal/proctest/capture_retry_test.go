package proctest_test

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/internal/relevance"
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

// strainSegments counts the ledger segments carrying the drafted strain's
// relation, which the recheck step appends.
func strainSegments(t *testing.T, store sdd.RelevanceStore) int {
	t.Helper()
	segments, err := store.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, segment := range segments {
		batch, err := relevance.Decode(segment)
		if err != nil {
			t.Fatal(err)
		}
		for _, reader := range batch.Readers {
			if reader.Pair == model.NewRelevancePair(captureRefID, captureRef2ID) && reader.Verdict.Relations&model.RelationContradicts != 0 {
				count++
			}
		}
	}
	return count
}

// A publish failing behind its written entry leaves the operation pending; its
// retry, in the same session or a resumed one, continues past every step that
// already committed: no second entry, no second completion, no second recheck
// item, and entry.written once.
func TestCapture_ARetryAfterAFailureBehindTheWriteKeepsOneEntry(t *testing.T) {
	for _, tc := range []struct {
		name    string
		resume  bool
		strains bool
		// failAt is the ledger append that fails: with a strain the recheck
		// step's comes first.
		failAt int
		// items is the recheck items and strain segments the capture leaves.
		items int
	}{
		{name: "same session", failAt: 1},
		{name: "resumed session", resume: true, failAt: 1},
		{name: "failure behind the recheck", strains: true, failAt: 2, items: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &failingRelevanceStore{RelevanceStore: local.NewMemoryRelevanceStore()}
			events := local.NewMemoryEventStore()
			finalizer := &captureCompletionCount{}
			scripted := &proctest.ScriptedHooks{Configured: []sdd.Hook{{HookConfig: model.HookConfig{Event: "entry.written", Command: "notify"}}}, Scripts: map[string]proctest.HookScript{}}
			world, session := newCaptureWorld(t, "retry-after-write", proctest.WithGroundingEnforced(), proctest.WithRelevance(store),
				proctest.WithEvents(events), proctest.WithHooks(scripted), proctest.WithFinalizers(finalizer))
			serve := session.Start(t, "capture", nil)
			instance := serve.Instance
			draft := captureDraft()
			if tc.strains {
				session.Gather(t, instance, captureRef2ID)
				draft["verdicts"] = []any{map[string]any{"ids": []any{captureRef2ID}, "code": model.VerdictCovered}}
				draft["strains"] = []any{strainOf(captureRef2ID, captureRefID, model.StrainContradicts)}
			} else {
				session.Gather(t, instance)
			}
			proctest.RequireStep(t, session.Report(t, instance, draft), "playback")
			before := graphEntries(t, world.GraphDir)
			store.failAt = tc.failAt
			failed, err := session.AnswerErr(t, instance, "playback", "confirm", nil, "write it")
			if err != nil {
				t.Fatal(err)
			}
			pending := failed.PendingOperation
			if pending == nil || store.failAt != 0 {
				t.Fatalf("a failure behind the write should leave the publish pending, served %+v", failed)
			}
			entryID := pending.Values["entryId"]
			proctest.LoadEntry(t, world.GraphDir, entryID)
			if tc.resume {
				session, _ = world.Resume(t, session.ID, "retry-after-write-resumed")
			}
			serve, err = session.WF.Advance(t.Context(), world.Identity, sdd.WorkflowAdvanceRequest{Instance: instance, RetryRef: pending.RetryRef})
			if err != nil {
				t.Fatal(err)
			}
			proctest.RequireStep(t, serve, "verifySummary")
			if got := graphEntries(t, world.GraphDir) - before; got != 1 {
				t.Fatalf("the retry should publish no second entry, the graph gained %d", got)
			}
			if finalizer.calls != 1 {
				t.Fatalf("the retry should continue past the committed publish, its completion ran %d times", finalizer.calls)
			}
			stored, err := events.Query(t.Context(), sdd.EventQuery{Channels: []string{model.RecheckChannel}, Types: []string{model.RecheckEventType}})
			if err != nil {
				t.Fatal(err)
			}
			if got := strainSegments(t, store); len(stored) != tc.items || got != tc.items {
				t.Fatalf("the retry should continue past the stored recheck, the strain left %d recheck items and %d ledger segments", len(stored), got)
			}
			written := scripted.RunsAt("entry.written")
			if len(written) != 1 || written[0].Payload["entry"] != entryID {
				t.Fatalf("entry.written should fire once naming the written entry, got %+v", written)
			}
			serve = session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
			if serve.Produced["entryId"] != entryID {
				t.Fatalf("the capture should produce the entry the first attempt wrote, got %v", serve.Produced["entryId"])
			}
		})
	}
}
