// Capture under grounding.enforce (20261109-004506-d-cpt-udz, slice 4): the
// assemble gate judges the capture's credited gather reading, the write step
// answers entries that landed after it, and a recorded bypass writes the
// entry with low confidence.
package proctest_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	llmrelevance "github.com/networkteam/sdd/pkg/llm/relevance"
	"github.com/networkteam/sdd/pkg/local"
)

const groundingUnreadID = "20250101-000000-s-tac-zzz"

func newGroundingWorld(t *testing.T, connID string) (*proctest.World, *proctest.Session) {
	t.Helper()
	return newCaptureWorld(t, connID, proctest.WithGroundingEnforced())
}

func requireDiagnostics(t *testing.T, serve *sdd.WorkflowServe, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !hasDiagnostic(serve, part) {
			t.Fatalf("the refusal should name %q, got %v", part, serve.Diagnostics)
		}
	}
}

func TestCaptureGrounding_RefusesUntilTheCaptureIsRead(t *testing.T) {
	_, session := newGroundingWorld(t, "grounding-unread")
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance

	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "assemble")
	requireDiagnostics(t, serve, "no credited reading", "call gather with instance "+instance)

	session.Gather(t, instance)
	serve = session.Report(t, instance, map[string]any{"confidence": "medium"})
	proctest.RequireStep(t, serve, "playback")
}

func TestCaptureGrounding_FullReadsEndAsRefOrVerdict(t *testing.T) {
	_, session := newGroundingWorld(t, "grounding-full")
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance

	session.Gather(t, instance, captureRef2ID)
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "assemble")
	requireDiagnostics(t, serve, "neither in refs nor in verdicts", captureRef2ID, "report each in refs or in verdicts")

	serve = session.Report(t, instance, map[string]any{"verdicts": []any{map[string]any{"ids": []any{captureRef2ID}, "code": model.VerdictCovered}}})
	proctest.RequireStep(t, serve, "playback")
}

func TestCaptureGrounding_VerdictNamesAListedEntry(t *testing.T) {
	_, session := newGroundingWorld(t, "grounding-verdict")
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance

	session.Gather(t, instance)
	draft := captureDraft()
	draft["verdicts"] = []any{map[string]any{"ids": []any{groundingUnreadID}, "code": model.VerdictOther}}
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "assemble")
	requireDiagnostics(t, serve, "never listed", groundingUnreadID, "call gather with instance "+instance+" and ids")

	_, err := session.ReportErr(t, instance, map[string]any{"verdicts": []any{map[string]any{"ids": []any{captureRef2ID}, "code": "unsure"}}})
	if err == nil || !strings.Contains(err.Error(), "verdict code") {
		t.Fatalf("a verdict code outside the closed set should be refused, got %v", err)
	}
}

func TestCaptureGrounding_EntryLandedSinceTheReadingMustBeListed(t *testing.T) {
	world, session := newGroundingWorld(t, "grounding-landed")
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance

	session.Gather(t, instance)
	landed := &model.Entry{ID: "20260602-090000-s-tac-lnd", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
		Summary: "A gap that landed during the reading.", Content: "A gap that landed during the reading."}
	proctest.WriteEntry(t, world.GraphDir, landed)

	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "assemble")
	requireDiagnostics(t, serve, "landed since the reading began", landed.ID, "call gather with instance "+instance)

	gathered := session.Gather(t, instance)
	if !strings.Contains(gathered.Text, "new since the reading began") || !strings.Contains(gathered.Text, landed.ID) {
		t.Fatalf("a later gather should list the landed entry:\n%s", gathered.Text)
	}
	serve = session.Report(t, instance, map[string]any{"confidence": "medium"})
	proctest.RequireStep(t, serve, "playback")
}

func TestCaptureGrounding_SeedsTheWidenReport(t *testing.T) {
	_, session := newGroundingWorld(t, "grounding-seed")
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance

	session.Gather(t, instance)
	serve = session.Report(t, instance, map[string]any{"entryKind": "gap"})
	proctest.RequireStep(t, serve, "assemble")
	if !strings.Contains(serve.Instructions, "Reading recorded by sdd gather:") || !strings.Contains(serve.Instructions, "all live: ") {
		t.Fatalf("the grounding lane should carry the seeded reading:\n%s", serve.Instructions)
	}

	draft := captureDraft()
	delete(draft, "widenReport")
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
}

// An entry landing between the confirm and the write reaches lateEntries,
// whatever its ID's age, and setting it aside returns to the write without a
// new confirm (D13, D37).
func TestCaptureGrounding_EntryLandingAfterTheConfirmReachesLateEntries(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   string
	}{
		{"a later entry", "20260603-090000-s-tac-lat"},
		{"an older entry arriving by merge", "20240101-090000-s-tac-mrg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world, session := newGroundingWorld(t, "grounding-late")
			world.LLM.PreflightFindings = []proctest.PreflightFinding{{Severity: "high", Category: "grounding", Observation: "the draft names no evidence"}}
			serve := session.Start(t, "capture", nil)
			instance := serve.Instance
			session.Gather(t, instance)
			serve = session.Report(t, instance, captureDraft())
			proctest.RequireStep(t, serve, "playback")
			serve = session.Answer(t, instance, "playback", "confirm", nil, "capture it")
			proctest.RequireStep(t, serve, "reviseOrOverride")

			late := &model.Entry{ID: tc.id, Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
				Summary: "A gap that landed after the confirm.", Content: "A gap that landed after the confirm."}
			proctest.WriteEntry(t, world.GraphDir, late)

			serve = session.Answer(t, instance, "reviseOrOverride", "override", nil, "write it anyway")
			proctest.RequireStep(t, serve, "lateEntries")
			if !strings.Contains(serve.Instructions, late.ID) || !strings.Contains(serve.Instructions, "A gap that landed after the confirm.") {
				t.Fatalf("lateEntries should list the landed entry as its brief:\n%s", serve.Instructions)
			}

			serve = session.Answer(t, instance, "lateEntries", "setAside",
				map[string]any{"lateVerdicts": []any{map[string]any{"ids": []any{late.ID}, "code": model.VerdictOtherScope}}}, "")
			proctest.RequireStep(t, serve, "verifySummary")
		})
	}
}

// An entry landing between the write step's check and the write itself
// leaves the capture unwritten and reaches lateEntries.
func TestCaptureGrounding_EntryLandingBeforeTheWriteIsCaught(t *testing.T) {
	world, session := newGroundingWorld(t, "grounding-race")
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")

	late := &model.Entry{ID: "20260603-100000-s-tac-rce", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
		Summary: "A gap that landed during the pre-flight.", Content: "A gap that landed during the pre-flight."}
	world.LLM.DuringPreflight = func() { proctest.WriteEntry(t, world.GraphDir, late) }

	serve = session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	proctest.RequireStep(t, serve, "lateEntries")
	if !strings.Contains(serve.Instructions, late.ID) {
		t.Fatalf("lateEntries should list the entry that landed before the write:\n%s", serve.Instructions)
	}
	if got := world.LLM.Calls("summarize"); got != 0 {
		t.Fatalf("the capture should not be written while an entry is unanswered, %d summaries ran", got)
	}
}

func TestCaptureGrounding_ReviseFromLateEntriesReopensPlayback(t *testing.T) {
	world, session := newGroundingWorld(t, "grounding-late-revise")
	world.LLM.PreflightFindings = []proctest.PreflightFinding{{Severity: "high", Category: "grounding", Observation: "the draft names no evidence"}}
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	session.Report(t, instance, captureDraft())
	session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	late := &model.Entry{ID: "20260603-110000-s-tac-rev", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
		Summary: "A gap that changes the draft.", Content: "A gap that changes the draft."}
	proctest.WriteEntry(t, world.GraphDir, late)
	serve = session.Answer(t, instance, "reviseOrOverride", "override", nil, "write it anyway")
	proctest.RequireStep(t, serve, "lateEntries")

	world.LLM.PreflightFindings = nil
	session.LogRead(t, "show", []string{late.ID}, nil)
	serve = session.Answer(t, instance, "lateEntries", "revise", map[string]any{"refs": []any{
		map[string]any{"id": captureRefID, "kind": "addresses"},
		map[string]any{"id": late.ID, "kind": "addresses"},
	}}, "")
	proctest.RequireStep(t, serve, "playback")
}

func TestCaptureGrounding_BypassWritesLowConfidence(t *testing.T) {
	world, session := newGroundingWorld(t, "grounding-bypass")
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance

	draft := captureDraft()
	draft["skipGrounding"] = true
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
	entry := confirmCapture(t, world, session, instance)
	if entry.Grounding != model.GroundingSkipped || entry.Confidence != "low" {
		t.Fatalf("a bypassed entry should carry grounding: skipped and confidence low, got %q and %q", entry.Grounding, entry.Confidence)
	}
}

func TestCaptureGrounding_ActorAndRoleDraftsAreExempt(t *testing.T) {
	world := proctest.NewWorld(t, proctest.WithGroundingEnforced(), proctest.WithEntries(append(captureFixtureEntries(), &model.Entry{
		ID: identityActorID, Type: model.TypeSignal, Kind: model.KindActor, Layer: model.LayerProcess, Canonical: "Christopher",
		Summary: "Christopher, the known actor role captures bind to.", Content: "Christopher, the known actor role captures bind to.",
	})...))
	for name, draft := range map[string]map[string]any{"actor": actorDraft(), "role": roleDraft()} {
		t.Run(name, func(t *testing.T) {
			session := world.Open(t, "grounding-exempt-"+name)
			serve := session.Start(t, "capture", nil)
			serve = session.Report(t, serve.Instance, draft)
			proctest.RequireStep(t, serve, "playback")
		})
	}
}

func TestCaptureGrounding_EnforcementOffPassesUnchanged(t *testing.T) {
	_, session := newCaptureWorld(t, "grounding-off")
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
}

// The credited reading and its seed replay from the session log: a resumed
// client continues the capture on the reading made before it attached.
func TestCaptureGrounding_ReadingSurvivesResume(t *testing.T) {
	world, session := newGroundingWorld(t, "grounding-resume")
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)

	resumed, _ := world.Resume(t, session.ID, "grounding-resumed")
	draft := captureDraft()
	delete(draft, "widenReport")
	serve = resumed.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
}

// Above grounding.brief_all_below the reading owes the searches and the
// listing of each draft topic; opening entries alone does not ground a draft.
func TestCaptureGrounding_SearchModeOwesTheSearchesAndListings(t *testing.T) {
	_, session := newCaptureWorld(t, "grounding-owed", proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, BriefAllBelow: 1}))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance

	session.Gather(t, instance, captureRefID)
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "assemble")
	requireDiagnostics(t, serve, "the reading lacks the semantic search, a keyword search, the listing of topic cli/ux", "call gather with instance "+instance)
}

// An entry another capture of the same dialogue wrote never counts as
// landed for a reading that began before it (5.5).
func TestCaptureGrounding_OwnCapturesNeverCountAsLanded(t *testing.T) {
	world, session := newGroundingWorld(t, "grounding-own")
	first := session.Start(t, "capture", nil).Instance
	session.Gather(t, first)

	second := session.Start(t, "capture", nil).Instance
	session.Gather(t, second)
	serve := session.Report(t, second, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	confirmCapture(t, world, session, second)

	serve = session.Report(t, first, captureDraft())
	proctest.RequireStep(t, serve, "playback")
}

// A published capture writes its reader records, keyed to the new entry,
// into the relevance ledger (5.7).
func TestCaptureGrounding_PublishStoresTheReaderVerdicts(t *testing.T) {
	store := local.NewMemoryRelevanceStore()
	world, session := newCaptureWorld(t, "grounding-ledger", proctest.WithGroundingEnforced(), proctest.WithRelevance(store))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance

	session.Gather(t, instance, captureRef2ID)
	draft := captureDraft()
	draft["verdicts"] = []any{map[string]any{"ids": []any{captureRef2ID}, "code": model.VerdictCovered}}
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
	entry := confirmCapture(t, world, session, instance)

	segments, err := store.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		t.Fatal(err)
	}
	referenced, ok := ledger.Reader(model.NewRelevancePair(entry.ID, captureRefID))
	if !ok || referenced.Code != model.ReaderReferenced {
		t.Fatalf("the ref should be stored as referenced by the new entry, got %+v, %v", referenced, ok)
	}
	opened, ok := ledger.Reader(model.NewRelevancePair(entry.ID, captureRef2ID))
	covered, _ := model.VerdictReasonByte(model.VerdictCovered)
	if !ok || opened.Code != model.ReaderOpened || opened.Reason != covered {
		t.Fatalf("the entry served in full and set aside should be stored as opened with its code, got %+v, %v", opened, ok)
	}
}

// countingScorer scores every candidate the same, or fails, and counts the
// candidates it was asked about.
type countingScorer struct {
	score  float64
	fail   bool
	scored int
}

func (s *countingScorer) Direction() llmrelevance.Direction { return llmrelevance.Both }
func (s *countingScorer) MaxBatch() int                     { return 0 }
func (s *countingScorer) MaxInputTokens() int               { return 0 }

func (s *countingScorer) Score(_ context.Context, request llmrelevance.Request) (llmrelevance.Result, error) {
	s.scored += len(request.Candidates)
	if s.fail {
		return llmrelevance.Result{}, errors.New("provider unavailable")
	}
	result := llmrelevance.Result{}
	for range request.Candidates {
		result.Scores = append(result.Scores, s.score)
	}
	return result, nil
}

// The prefilter's scores ride the capture's reading: a later gather reuses
// them instead of scoring again, and publish stores them keyed to the new
// entry; a failed pass stores none (5.8).
func TestCaptureGrounding_PrefilterScoresRideTheReadingToTheLedger(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fail   bool
		stored bool
	}{{"scored", false, true}, {"provider down", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			store := local.NewMemoryRelevanceStore()
			scorer := &countingScorer{score: 0.25, fail: tc.fail}
			world, session := newCaptureWorld(t, "grounding-prefilter",
				proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1}),
				proctest.WithRelevance(store), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: "test:counting"}))
			serve := session.Start(t, "capture", nil)
			instance := serve.Instance
			session.Report(t, instance, map[string]any{"body": "A tactical gap: the fixture observes something.", "entryKind": "gap"})
			session.Gather(t, instance)
			first := scorer.scored
			if first == 0 {
				t.Fatal("the prefilter should rate the reading's candidates")
			}
			session.Gather(t, instance)
			if !tc.fail && scorer.scored != first {
				t.Fatalf("a second gather scored %d more candidates: the reading's scores should be reused", scorer.scored-first)
			}
			serve = session.Report(t, instance, captureDraft())
			proctest.RequireStep(t, serve, "playback")
			entry := confirmCapture(t, world, session, instance)

			segments, err := store.Segments(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := relevance.Fold(segments)
			if err != nil {
				t.Fatal(err)
			}
			_, scored := ledger.Score(model.NewRelevancePair(entry.ID, captureRef2ID), entry.ID, "test:counting")
			if scored != tc.stored {
				t.Fatalf("score stored = %v: a successful pass should store its scores, a failed one none", scored)
			}
		})
	}
}
