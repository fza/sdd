package proctest_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/local"
	"github.com/networkteam/sdd/test"
)

func floorLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "- "+model.SetAsideFloor+":") {
			return line
		}
	}
	return ""
}

// A capture's far floor verdicts ride its reading: a later gather of the
// same draft scores nothing again and sets the same candidates aside. A
// set-aside the audit showed blind and the draft then references reaches
// the ledger as a miss of the floor, and no far entry's score reaches it.
func TestCaptureGrounding_FarFloorVerdictsRideTheReadingToTheLedger(t *testing.T) {
	const judge = "test:scripted"
	var candidates []string
	var entries []*model.Entry
	for i := range 6 {
		id := fmt.Sprintf("20260201-1000%02d-s-tac-omg", i)
		candidates = append(candidates, id)
		text := fmt.Sprintf("An omega gap number %d.", i)
		entries = append(entries, &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Summary: text, Content: text})
	}
	for i := range 3 {
		text := fmt.Sprintf("A far gap number %d.", i)
		entries = append(entries, &model.Entry{ID: fmt.Sprintf("20260202-1000%02d-s-tac-far", i), Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Summary: text, Content: text})
	}
	store := local.NewMemoryRelevanceStore()
	scorer := &test.ScriptedScorer{Default: 0.1}
	world, session := newCaptureWorld(t, "grounding-floor",
		proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1, BriefAllBelow: 1}),
		proctest.WithRelevance(store), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: judge}),
		proctest.WithEmbedder(kappaEmbedder()), proctest.WithEntries(entries...))
	world.LLM.GatherQuery, world.LLM.GatherTerm = "omega gaps", "omega"
	instance := session.Start(t, "capture", nil).Instance
	draft := captureDraft()
	draft["body"] = "A tactical gap: the fixture observes something about omega."
	session.Report(t, instance, draft)

	first := session.Gather(t, instance)
	setAside := floorLine(first.Text)
	if strings.Count(setAside, "-s-tac-omg") != 5 || len(first.SummaryIDs) != 1 || !slices.Contains(candidates, first.SummaryIDs[0]) {
		t.Fatalf("every candidate rated at the floor should be set aside by it, one of them shown blind:\n%s", first.Text)
	}
	sample := first.SummaryIDs[0]
	scored := scorer.Scored()
	var far []string
	for _, id := range scored {
		if !slices.Contains(candidates, id) && !slices.Contains(far, id) {
			far = append(far, id)
		}
	}
	if len(far) != model.FarFloorSize {
		t.Fatalf("far entries %v: the judge calls should carry three entries beside the candidates", far)
	}

	second := session.Gather(t, instance)
	if len(scorer.Scored()) != len(scored) {
		t.Fatalf("a second gather scored %d more lines: the reading's scores should be reused", len(scorer.Scored())-len(scored))
	}
	if floorLine(second.Text) != setAside || !slices.Equal(second.SummaryIDs, first.SummaryIDs) {
		t.Fatalf("a second gather should keep the verdicts the reading holds:\n%s", second.Text)
	}

	session.LogRead(t, "show", []string{sample}, nil)
	draft["refs"] = append(draft["refs"].([]any), map[string]any{"id": sample, "kind": "addresses", "desc": "bears on it"})
	proctest.RequireStep(t, session.Report(t, instance, draft), "playback")
	entry := confirmCapture(t, world, session, instance)

	segments, err := store.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		t.Fatal(err)
	}
	verdict, ok := ledger.Reader(model.NewRelevancePair(entry.ID, sample))
	if !ok || verdict.Code != model.ReaderReferenced || !verdict.Audit || !verdict.Floor {
		t.Fatalf("the referenced sample should be stored as an audit miss of the floor, got %+v, %v", verdict, ok)
	}
	for _, id := range candidates {
		if _, scored := ledger.Score(model.NewRelevancePair(entry.ID, id), entry.ID, judge); !scored {
			t.Fatalf("the score of candidate %s should reach the ledger", id)
		}
	}
	for _, id := range far {
		if _, scored := ledger.Score(model.NewRelevancePair(entry.ID, id), entry.ID, judge); scored {
			t.Fatalf("the score of far entry %s should never reach the ledger", id)
		}
	}
	if misses := model.JudgeFloorHealth(ledger.Labels(judge)); misses.Misses != 1 || misses.Counted != 1 {
		t.Fatalf("floor health %+v: the referenced sample should count against the floor", misses)
	}
}
