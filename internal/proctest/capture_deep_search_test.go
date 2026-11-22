package proctest_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/test"
)

// unlistedKappa shares the final draft's word kappa and stands under no
// topic, so no check of a capture drafted without kappa lists it.
const unlistedKappa = "20260605-120000-s-tac-unl"

func unlistedKappaEntry() *model.Entry {
	return &model.Entry{
		ID: unlistedKappa, Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
		Summary: "A kappa gap nobody listed.", Content: "A kappa gap nobody listed.",
	}
}

// The verifier's reading runs the capture's searches again against the
// final draft: an entry only the search with the final draft's text finds
// joins its lines.
func TestCaptureDeep_TheVerifiersReadingSearchesTheFinalDraft(t *testing.T) {
	world, session := newDeepWorld(t, "zed-agent", sdd.VerifierPolicy{},
		proctest.WithEntries(append(laneEntries(), unlistedKappaEntry())...), proctest.WithEmbedder(kappaEmbedder()))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Report(t, instance, keptDraft("A tactical gap: the fixture observes something."))
	checked := session.Gather(t, instance)
	if strings.Contains(checked.Text, unlistedKappa) {
		t.Fatalf("the drafter's check should not list the kappa gap:\n%s", checked.Text)
	}
	serve = session.Report(t, instance, answerMarked(checked.Text, kappaDraft()))
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, instance, "playback", "confirm", nil, "write it")
	proctest.RequireStep(t, serve, "deepDispatch")

	reading := verifierReading(t, dispatchDeep(t, world, session, instance))
	if readingLine(reading, unlistedKappa) == "" {
		t.Fatalf("the verifier's reading should list what the search with the final draft finds:\n%s", reading)
	}
}

// A judgement the drafter's check gave stays with the capture: the
// verifier's reading uses it after the draft changed and asks the judge
// only about what holds none.
func TestCaptureDeep_TheVerifiersReadingKeepsTheChecksJudgements(t *testing.T) {
	scorer := &test.ScriptedScorer{Scores: map[string]float64{laneRelevant: 0.9}, Default: 0.6}
	world, session := newDeepWorld(t, "zed-agent", sdd.VerifierPolicy{},
		proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1}),
		proctest.WithEntries(laneEntries()...), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: gatherJudge}))
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Report(t, instance, keptDraft("A tactical gap: the fixture observes something."))
	checked := session.Gather(t, instance)
	if !slices.Contains(scorer.Scored(), laneRelevant) {
		t.Fatalf("the drafter's check should judge the lane entries:\n%s", checked.Text)
	}
	asked := len(scorer.Scored())
	draft := captureDraft()
	draft["body"] = "A tactical gap: the fixture observes something else."
	serve = session.Report(t, instance, answerMarked(checked.Text, draft))
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, instance, "playback", "confirm", nil, "write it")
	proctest.RequireStep(t, serve, "deepDispatch")

	reading := verifierReading(t, dispatchDeep(t, world, session, instance))
	if again := askedSince(scorer, asked); slices.Contains(again, laneRelevant) {
		t.Fatalf("asked again about %v: a bearing judgement should stay for the verifier's reading", again)
	}
	if !strings.HasPrefix(readingLine(reading, laneRelevant), "! .90") {
		t.Fatalf("the verifier's reading should place the entry by its kept judgement:\n%s", reading)
	}
}
