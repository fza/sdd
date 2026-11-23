// Capture publish under grounding enforcement: every entry the reading
// showed ends in one of four states, and a later entry referencing both
// members of an implicitly dismissed pair flips it to bearing.
package proctest_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/internal/relevance"
	test "github.com/networkteam/sdd/internal/testsupport"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/local"
)

func foldLedger(t *testing.T, store sdd.RelevanceStore) *model.RelevanceLedger {
	t.Helper()
	segments, err := store.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

// A brief the reading printed to judge and the capture left unanswered is
// stored as an implicit dismissal of the published entry.
func TestCaptureDismissals_UnansweredBriefIsDismissedImplicitly(t *testing.T) {
	store := local.NewMemoryRelevanceStore()
	world, session := newCaptureWorld(t, "dismissals-implicit", proctest.WithGroundingEnforced(), proctest.WithRelevance(store))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	gathered := session.Gather(t, instance)
	if !slices.Contains(gathered.SummaryIDs, captureRef2ID) {
		t.Fatalf("the small fixture graph should print every live brief to judge:\n%s", gathered.Text)
	}
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	entry := confirmCapture(t, world, session, instance)

	ledger := foldLedger(t, store)
	if verdict, ok := ledger.Reader(model.NewRelevancePair(entry.ID, captureRefID)); !ok || verdict.Code != model.ReaderReferenced {
		t.Fatalf("the ref should stay referenced, got %+v, %v", verdict, ok)
	}
	if verdict, ok := ledger.Reader(model.NewRelevancePair(entry.ID, captureRef2ID)); !ok || verdict.Code != model.ReaderImplicit {
		t.Fatalf("the brief left unanswered should be dismissed implicitly, got %+v, %v", verdict, ok)
	}
}

// Behind a calibrated prefilter the set-aside entries are listed only by ID
// and get no record; the blind audit sample, shown and left unanswered, is
// an implicit dismissal carrying the audit bit.
func TestCaptureDismissals_SetAsidesStayUnlabelledAndSamplesAreImplicit(t *testing.T) {
	const judge = "test:counting"
	var seed model.RelevanceBatch
	for i := range 189 {
		pair := model.NewRelevancePair(fmt.Sprintf("20250101-%02d%02d00-s-tac-pos", i/60, i%60), "20250601-000000-d-tac-sub")
		seed.Readers = append(seed.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}})
		seed.Scores = append(seed.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: judge, Score: 200}})
	}
	store := local.NewMemoryRelevanceStore()
	if err := store.Append(t.Context(), relevance.Encode(seed)); err != nil {
		t.Fatal(err)
	}
	world, session := newCaptureWorld(t, "dismissals-audit",
		proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1}),
		proctest.WithRelevance(store), proctest.WithPrefilter(sdd.Prefilter{Scorer: &countingScorer{score: 0}, Judge: judge}))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Report(t, instance, map[string]any{"body": "A tactical gap: the fixture observes something.", "entryKind": "gap"})
	gathered := session.Gather(t, instance)
	samples := slices.DeleteFunc(slices.Clone(gathered.SummaryIDs), func(id string) bool { return id == captureRefID })
	if len(samples) == 0 || slices.Contains(gathered.SummaryIDs, captureRef2ID) {
		t.Fatalf("every candidate scored low, so the lines to judge should be audit samples and the fixture signal set aside:\n%s", gathered.Text)
	}
	asides := []string{captureRef2ID}
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	entry := confirmCapture(t, world, session, instance)

	ledger := foldLedger(t, store)
	for _, id := range samples {
		if verdict, ok := ledger.Reader(model.NewRelevancePair(entry.ID, id)); !ok || verdict.Code != model.ReaderImplicit || !verdict.Audit {
			t.Errorf("the unanswered sample %s should be an implicit dismissal with the audit bit, got %+v, %v", id, verdict, ok)
		}
	}
	for _, id := range asides {
		if verdict, ok := ledger.Reader(model.NewRelevancePair(entry.ID, id)); ok {
			t.Errorf("the set-aside %s was never shown and should get no record, got %+v", id, verdict)
		}
	}
}

// A published entry that references both members of an implicitly
// dismissed pair flips the pair to bearing.
func TestCaptureDismissals_PublishFlipsAPairTheEntryJoins(t *testing.T) {
	pair := model.NewRelevancePair(captureRefID, captureRef2ID)
	store := local.NewMemoryRelevanceStore()
	if err := store.Append(t.Context(), relevance.Encode(model.RelevanceBatch{Readers: []model.ReaderRecord{{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderImplicit}}}})); err != nil {
		t.Fatal(err)
	}
	world, session := newCaptureWorld(t, "dismissals-flip", proctest.WithGroundingEnforced(), proctest.WithRelevance(store))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance, captureRef2ID)
	draft := captureDraft()
	draft["refs"] = append(draft["refs"].([]any), map[string]any{"id": captureRef2ID, "kind": "addresses", "desc": "observes it"})
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
	confirmCapture(t, world, session, instance)

	verdict, ok := foldLedger(t, store).Reader(pair)
	if !ok || verdict.Code != model.ReaderImplicit || !verdict.Flipped || model.LabelKind(verdict) != model.LabelPositive {
		t.Fatalf("the implicit pair both refs join should flip to bearing, got %+v, %v", verdict, ok)
	}
}

// The deep verifier's reading marks a brief the drafter was shown and left
// unanswered as implicitly dismissed, and leaves an answered one unmarked.
func TestCaptureDismissals_DeepReadingMarksImplicitDismissals(t *testing.T) {
	world, session := newDeepWorld(t, "dismissals-deep", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	reading := verifierReading(t, x)
	if !strings.Contains(reading, "ID: "+captureRef2ID+" | kind: gap | Status: open\nSummary: A signal no fixture session has read in full.\nRole in the reading: implicitly dismissed\n") {
		t.Fatalf("the brief left unanswered should carry the implicit dismissal role:\n%s", reading)
	}
	if !strings.Contains(reading, "implicitly dismissed (shown, left unanswered") {
		t.Fatalf("the verifier's instructions should describe the implicit role:\n%s", reading)
	}
}

// The prose verifier's reading marks the same entries.
func TestCaptureDismissals_LightReadingMarksImplicitDismissals(t *testing.T) {
	world, session := newLightWorld(t, "dismissals-light")
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	session.Answer(t, instance, "playback", "confirm", nil, "write it")
	prompt := world.LLM.Prompts(llm.PurposePreflight)[0].UserPrompt
	if !strings.Contains(prompt, "ID: "+captureRef2ID+"\n") || !strings.Contains(prompt, "Role in the reading: implicitly dismissed") {
		t.Fatalf("the light reading should mark the brief left unanswered:\n%s", prompt)
	}
}

// The assemble instructions name the four states every shown entry ends in.
func TestCaptureDismissals_AssembleNamesTheFourStates(t *testing.T) {
	_, session := newGroundingWorld(t, "dismissals-served")
	serve := session.Start(t, "capture", nil)
	proctest.RequireStep(t, serve, "assemble")
	if !strings.Contains(serve.Instructions, "every entry shown ends referenced, opened, dismissed in `verdicts`") || !strings.Contains(serve.Instructions, "or else implicitly at publish") {
		t.Fatalf("the assemble step should state the four states a shown entry ends in:\n%s", serve.Instructions)
	}
}

// The deep reading's one-line lane marks an entry the drafter was shown and
// left unanswered, keeping one line per entry.
func TestCaptureDismissals_DeepLaneMarksImplicitDismissals(t *testing.T) {
	scorer := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9}}
	world, session := newDeepWorld(t, "dismissals-deep-lane", sdd.VerifierPolicy{}, proctest.WithEntries(laneEntries()...),
		proctest.WithRelevance(laneLedger(t, true)), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: laneJudge}))
	x := startDeep(t, world, session, captureDraft())
	reading := verifierReading(t, x)
	_, lane, ok := strings.Cut(reading, "## Listed below the prefilter's threshold (")
	if !ok {
		t.Fatalf("the calibrated reading should serve the one-line lane:\n%s", reading)
	}
	var marked int
	for line := range strings.Lines(lane) {
		id, title, isEntry := strings.Cut(strings.TrimSuffix(line, "\n"), "  ")
		if !isEntry || !strings.HasPrefix(id, "20260603-") {
			continue
		}
		if !strings.HasPrefix(title, "Low directive ") || !strings.HasSuffix(title, ".  (implicitly dismissed)") {
			t.Fatalf("a lane entry shown and left unanswered should carry the implicit mark on its line, got %q", line)
		}
		marked++
	}
	if marked == 0 {
		t.Fatalf("the lane should hold entries shown and left unanswered:\n%s", lane)
	}
}
