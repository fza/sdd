// A capture keeps the ranking judge's judgements when its draft changes: a
// later check sends the judge only the candidates without one and the low
// judgements due a second look (20261121-233807-d-tac-4iw).
package proctest_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/test"
)

// keptWorld is a capture world whose judge is scorer, over the capture
// fixtures and 25 kappa and 25 lambda gaps under the draft's topic, the
// kappa ones near a draft about kappa.
func keptWorld(t *testing.T, connID string, scorer *test.ScriptedScorer) (*proctest.World, *proctest.Session) {
	t.Helper()
	var entries []*model.Entry
	for _, word := range []string{"kappa", "lambda"} {
		for i := range 25 {
			text := fmt.Sprintf("A gap on %s matters, number %d.", word, i)
			entries = append(entries, &model.Entry{
				ID: fmt.Sprintf("20260501-1%05d-s-tac-%c%02d", i, word[0], i), Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
				Summary: text, Content: text, Topics: proctest.MustTopics("cli/ux"),
			})
		}
	}
	world, session := newCaptureWorld(t, connID,
		proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1}),
		proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: "test:scripted"}),
		proctest.WithEmbedder(kappaEmbedder()), proctest.WithEntries(entries...))
	world.GatherTopics = nil
	return world, session
}

func keptDraft(body string) map[string]any {
	return map[string]any{"body": body, "entryKind": "gap", "topics": []any{"cli/ux"}}
}

// askedSince returns the IDs scorer was asked about after its first n asks.
func askedSince(scorer *test.ScriptedScorer, n int) []string {
	return slices.Compact(slices.Sorted(slices.Values(scorer.Scored()[n:])))
}

// A judgement that places its entry as likely bearing is kept as it is: the
// check after a change to the draft sends the judge none of them, and its
// lines carry the kept scores.
func TestCaptureKeptJudgements_ABearingJudgementStays(t *testing.T) {
	scorer := &test.ScriptedScorer{Default: 0.9}
	_, session := keptWorld(t, "kept-bearing", scorer)
	instance := session.Start(t, "capture", nil).Instance

	session.Report(t, instance, keptDraft("A tactical gap: the fixture observes kappa."))
	first := session.Gather(t, instance)
	asked := len(scorer.Scored())
	if asked == 0 || !strings.Contains(first.Text, ".90") {
		t.Fatalf("the first check should judge its candidates:\n%s", first.Text)
	}
	session.Report(t, instance, keptDraft("A tactical gap: the fixture observes kappa twice."))
	again := session.Gather(t, instance)
	if len(scorer.Scored()) != asked {
		t.Fatalf("asked again about %v: a bearing judgement should stay with the capture", askedSince(scorer, asked))
	}
	if !strings.Contains(again.Text, ".90") {
		t.Fatalf("the later check should place its lines by the kept judgements:\n%s", again.Text)
	}
}

// A low judgement is judged again when the next check's merged order ranks
// it among the first 20; one further down keeps its judgement, until the
// draft moves: then every low judgement is judged again.
func TestCaptureKeptJudgements_ASecondLookForLowJudgements(t *testing.T) {
	const deepKappa, lateLambda = "20260501-100024-s-tac-k24", "20260501-100024-s-tac-l24"
	scorer := &test.ScriptedScorer{Default: 0.2}
	_, session := keptWorld(t, "kept-low", scorer)
	instance := session.Start(t, "capture", nil).Instance

	session.Report(t, instance, keptDraft("A tactical gap: the fixture observes kappa."))
	session.Gather(t, instance)
	asked := len(scorer.Scored())

	session.Report(t, instance, keptDraft("A tactical gap: the fixture observes kappa again."))
	session.Gather(t, instance)
	rejudged := askedSince(scorer, asked)
	if len(rejudged) == 0 || slices.Contains(rejudged, lateLambda) {
		t.Fatalf("asked again about %v: only the low judgements among the first 20 should be judged again", rejudged)
	}
	asked = len(scorer.Scored())

	session.Report(t, instance, keptDraft("A tactical gap: the fixture observes lambda."))
	session.Gather(t, instance)
	if rejudged := askedSince(scorer, asked); !slices.Contains(rejudged, deepKappa) || !slices.Contains(rejudged, lateLambda) {
		t.Fatalf("asked again about %v: a moved draft should have every low judgement judged again", rejudged)
	}
}

// A check passed rejudge sends the judge every candidate again, the kept
// judgements included.
func TestCaptureKeptJudgements_RejudgeJudgesEveryCandidateAgain(t *testing.T) {
	scorer := &test.ScriptedScorer{Default: 0.9}
	world, session := keptWorld(t, "kept-rejudge", scorer)
	instance := session.Start(t, "capture", nil).Instance

	session.Report(t, instance, keptDraft("A tactical gap: the fixture observes kappa."))
	session.Gather(t, instance)
	asked := len(scorer.Scored())
	session.Gather(t, instance)
	if len(scorer.Scored()) != asked {
		t.Fatalf("asked again about %v: an unchanged draft should reuse every judgement", askedSince(scorer, asked))
	}
	_, err := session.WF.Gather(t.Context(), world.Identity, sdd.WorkflowGatherRequest{Instance: instance, Query: world.GatherQuery, Terms: world.GatherTerms, Rejudge: true, SyncMode: sdd.SearchSyncLocal})
	if err != nil {
		t.Fatal(err)
	}
	again := askedSince(scorer, asked)
	for _, word := range []string{"k", "l"} {
		for i := range 25 {
			if id := fmt.Sprintf("20260501-1%05d-s-tac-%s%02d", i, word, i); !slices.Contains(again, id) {
				t.Fatalf("%s not asked again: rejudge should judge every candidate", id)
			}
		}
	}
}
