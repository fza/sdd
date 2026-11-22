package proctest_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/embed"
	"github.com/networkteam/sdd/pkg/local"
	"github.com/networkteam/sdd/test"
)

func floorCount(text string) int {
	return setAsideCount(text, model.SetAsideFloor)
}

// A candidate next to an entry the draft references never falls below the
// floor; one next to an entry the draft only dismisses does, like any other.
func TestCaptureGrounding_FarFloorSparesTheNeighbourhoodOfReferencesAlone(t *testing.T) {
	const (
		dismissed     = "20260201-100000-s-tac-dis"
		nearReference = "20260201-100001-s-tac-nrf"
		nearDismissed = "20260201-100002-s-tac-nds"
	)
	gap := func(id, text string, refs ...string) *model.Entry {
		e := &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Summary: text, Content: text}
		for _, ref := range refs {
			e.Refs = append(e.Refs, model.Ref{ID: ref, Kind: model.RefKindAddresses, Desc: "bears on it"})
		}
		return e
	}
	entries := []*model.Entry{
		gap(dismissed, "A gap the draft dismisses."),
		gap(nearReference, "An omega gap next to the reference.", captureRefID),
		gap(nearDismissed, "An omega gap next to the dismissed one.", dismissed),
	}
	for i := range 2 {
		entries = append(entries, gap(fmt.Sprintf("20260201-1001%02d-s-tac-omg", i), fmt.Sprintf("An omega gap number %d.", i)))
	}
	for i := range 3 {
		entries = append(entries, gap(fmt.Sprintf("20260202-1000%02d-s-tac-far", i), fmt.Sprintf("A far gap number %d.", i)))
	}
	world, session := newCaptureWorld(t, "grounding-floor-near",
		proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1}),
		proctest.WithPrefilter(sdd.Prefilter{Scorer: lowScorer(entries), Judge: "test:scripted"}),
		proctest.WithEmbedder(floorEmbedder), proctest.WithEntries(entries...))
	world.GatherQuery, world.GatherTerms = "omega gaps", []string{"omega"}
	instance := session.Start(t, "capture", nil).Instance
	session.Gather(t, instance, dismissed)
	draft := captureDraft()
	draft["body"] = "A tactical gap: the fixture observes something about omega."
	draft["verdicts"] = []any{map[string]any{"ids": []any{dismissed}, "code": model.VerdictCovered}}
	session.Report(t, instance, draft)

	gathered := session.Gather(t, instance)
	floored := floorCount(gathered.Text)
	if !strings.HasPrefix(readingLine(gathered.Text, nearReference), "  .10") {
		t.Fatalf("a candidate next to the draft's reference should stay to judge:\n%s", gathered.Text)
	}
	if floored != 2 || len(fixtureShown(entries, gathered.SummaryIDs)) != 2 {
		t.Fatalf("the candidate next to the dismissed entry should fall below the floor with the two plain ones, one of the three shown blind:\n%s", gathered.Text)
	}
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
	scorer := lowScorer(entries)
	world, session := newCaptureWorld(t, "grounding-floor",
		proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1}),
		proctest.WithRelevance(store), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: judge}),
		proctest.WithEmbedder(floorEmbedder), proctest.WithEntries(entries...))
	world.GatherQuery, world.GatherTerms = "omega gaps", []string{"omega"}
	instance := session.Start(t, "capture", nil).Instance
	draft := captureDraft()
	draft["body"] = "A tactical gap: the fixture observes something about omega."
	session.Report(t, instance, draft)

	first := session.Gather(t, instance)
	floored := floorCount(first.Text)
	shown := fixtureShown(entries, first.SummaryIDs)
	if floored != 5 || len(shown) != 1 || !slices.Contains(candidates, shown[0]) {
		t.Fatalf("every candidate rated at the floor should be set aside by it, one of them shown blind:\n%s", first.Text)
	}
	sample := shown[0]
	scored := scorer.Scored()
	var far []string
	for _, id := range scored {
		if !slices.Contains(candidates, id) && !slices.Contains(far, id) && strings.HasSuffix(id, "-s-tac-far") {
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
	if floorCount(second.Text) != floored || !slices.Equal(second.SummaryIDs, first.SummaryIDs) {
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

// A verdict the reading holds gives way to what the floor never overrides: a
// reference the draft adds next to the candidate, and a floor its judge's
// audit turned off since.
func TestCaptureGrounding_HeldFarFloorVerdictsYieldToALaterReferenceAndASwitchOff(t *testing.T) {
	const judge = "test:scripted"
	gap := func(id, text string, refs ...string) *model.Entry {
		e := &model.Entry{ID: id, Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Summary: text, Content: text}
		for _, ref := range refs {
			e.Refs = append(e.Refs, model.Ref{ID: ref, Kind: model.RefKindAddresses, Desc: "bears on it"})
		}
		return e
	}
	pairs := make([][2]string, 3)
	var entries []*model.Entry
	for i := range pairs {
		pairs[i] = [2]string{fmt.Sprintf("20260201-1000%02d-s-tac-omg", 2*i), fmt.Sprintf("20260201-1000%02d-s-tac-omg", 2*i+1)}
		entries = append(entries, gap(pairs[i][0], fmt.Sprintf("An omega gap number %d.", 2*i)), gap(pairs[i][1], fmt.Sprintf("An omega gap number %d.", 2*i+1), pairs[i][0]))
	}
	for i := range 3 {
		entries = append(entries, gap(fmt.Sprintf("20260202-1000%02d-s-tac-far", i), fmt.Sprintf("A far gap number %d.", i)))
	}
	for _, tc := range []struct {
		name  string
		after func(t *testing.T, store *local.MemoryRelevanceStore, session *proctest.Session, instance string, draft map[string]any, target string)
		check func(t *testing.T, second sdd.GatherResult, neighbour string, other [2]string)
	}{
		{
			name: "a reference the draft adds",
			after: func(t *testing.T, _ *local.MemoryRelevanceStore, session *proctest.Session, instance string, draft map[string]any, target string) {
				session.LogRead(t, "show", []string{target}, nil)
				draft["refs"] = append(draft["refs"].([]any), map[string]any{"id": target, "kind": "addresses", "desc": "bears on it"})
				session.Report(t, instance, draft)
			},
			check: func(t *testing.T, second sdd.GatherResult, neighbour string, other [2]string) {
				floored := floorCount(second.Text)
				if !strings.HasPrefix(readingLine(second.Text, neighbour), "  .") {
					t.Fatalf("the candidate next to the new reference should return to the lines to judge:\n%s", second.Text)
				}
				if floored == 0 || slices.Contains(second.SummaryIDs, other[0]) || slices.Contains(second.SummaryIDs, other[1]) {
					t.Fatalf("a candidate far from the new reference should keep its verdict:\n%s", second.Text)
				}
			},
		},
		{
			name: "a floor turned off",
			after: func(t *testing.T, store *local.MemoryRelevanceStore, _ *proctest.Session, _ string, _ map[string]any, _ string) {
				var misses model.RelevanceBatch
				topicOnly, _ := model.VerdictReasonByte(model.VerdictTopicOnly)
				for i := range 60 {
					pair := model.NewRelevancePair(fmt.Sprintf("20250101-%06d-s-tac-aud", i), fmt.Sprintf("20250601-%06d-d-tac-sub", i))
					verdict := model.ReaderVerdict{Code: model.ReaderDismissed, Reason: topicOnly, Audit: true, Floor: true}
					if i < 2 {
						verdict = model.ReaderVerdict{Code: model.ReaderReferenced, Audit: true, Floor: true}
					}
					misses.Readers = append(misses.Readers, model.ReaderRecord{Pair: pair, Verdict: verdict})
					misses.Scores = append(misses.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: judge, Score: 5}})
				}
				if err := store.Append(t.Context(), relevance.Encode(misses)); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, second sdd.GatherResult, _ string, _ [2]string) {
				if floorCount(second.Text) != 0 || len(fixtureShown(entries, second.SummaryIDs)) != 2*len(pairs) {
					t.Fatalf("a floor that is off should set nothing aside, held verdicts included:\n%s", second.Text)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := local.NewMemoryRelevanceStore()
			scorer := lowScorer(entries)
			world, session := newCaptureWorld(t, "grounding-floor-held",
				proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1}),
				proctest.WithRelevance(store), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: judge}),
				proctest.WithEmbedder(floorEmbedder), proctest.WithEntries(entries...))
			world.GatherQuery, world.GatherTerms = "omega gaps", []string{"omega"}
			instance := session.Start(t, "capture", nil).Instance
			draft := captureDraft()
			draft["body"] = "A tactical gap: the fixture observes something about omega."
			session.Report(t, instance, draft)

			first := session.Gather(t, instance)
			shown := fixtureShown(entries, first.SummaryIDs)
			if floorCount(first.Text) != 2*len(pairs)-1 || len(shown) != 1 {
				t.Fatalf("every candidate rated at the floor should be set aside by it, one of them shown blind:\n%s", first.Text)
			}
			var free [][2]string
			for _, pair := range pairs {
				if !slices.Contains(pair[:], shown[0]) {
					free = append(free, pair)
				}
			}
			scored := len(scorer.Scored())
			tc.after(t, store, session, instance, draft, free[0][0])
			second := session.Gather(t, instance)
			if len(scorer.Scored()) != scored {
				t.Fatalf("the second gather scored %d more lines: the reading's scores should be reused", len(scorer.Scored())-scored)
			}
			tc.check(t, second, free[0][1], free[1])
		})
	}
}

// floorEmbedder puts the omega and far gaps opposite every query and every
// other entry, so the search with the draft's text lists the base entries
// and each gap is found by the reads the test names alone.
var floorEmbedder = embed.EmbedderFunc{Space: "test:floor", Run: func(_ context.Context, request embed.Request) (embed.Result, error) {
	result := embed.Result{Identity: llm.Identity{Provider: "test", Model: "floor"}}
	for _, text := range request.Texts {
		vector := []float32{1, 0}
		if request.Purpose != embed.PurposeQuery && (strings.Contains(text, "omega gap") || strings.Contains(text, "far gap")) {
			vector = []float32{-1, 0}
		}
		result.Vectors = append(result.Vectors, vector)
	}
	return result, nil
}}

// lowScorer rates the fixture's own entries 0.1, at the floor its far gaps
// set, and every other entry, the base entries the binary ships, 0.9.
func lowScorer(entries []*model.Entry) *test.ScriptedScorer {
	scores := map[string]float64{}
	for _, e := range entries {
		scores[e.ID] = 0.1
	}
	return &test.ScriptedScorer{Default: 0.9, Scores: scores}
}

// fixtureShown are the IDs of shown that name one of entries.
func fixtureShown(entries []*model.Entry, shown []string) []string {
	var out []string
	for _, id := range shown {
		if slices.ContainsFunc(entries, func(e *model.Entry) bool { return e.ID == id }) {
			out = append(out, id)
		}
	}
	return out
}
