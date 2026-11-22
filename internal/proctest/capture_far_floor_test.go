package proctest_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

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
		proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1, BriefAllBelow: 1}),
		proctest.WithPrefilter(sdd.Prefilter{Scorer: &test.ScriptedScorer{Default: 0.1}, Judge: "test:scripted"}),
		proctest.WithEmbedder(kappaEmbedder()), proctest.WithEntries(entries...))
	world.LLM.GatherQuery, world.LLM.GatherTerm = "omega gaps", "omega"
	instance := session.Start(t, "capture", nil).Instance
	session.Gather(t, instance, dismissed)
	draft := captureDraft()
	draft["body"] = "A tactical gap: the fixture observes something about omega."
	draft["verdicts"] = []any{map[string]any{"ids": []any{dismissed}, "code": model.VerdictCovered}}
	session.Report(t, instance, draft)

	gathered := session.Gather(t, instance)
	setAside := floorLine(gathered.Text)
	if !slices.Contains(gathered.SummaryIDs, nearReference) || strings.Contains(setAside, nearReference) {
		t.Fatalf("a candidate next to the draft's reference should stay to judge:\n%s", gathered.Text)
	}
	if strings.Count(setAside, "20260201-") != 3 || len(gathered.SummaryIDs) != 1 {
		t.Fatalf("the candidate next to the dismissed entry should fall below the floor with the two plain ones, none shown blind:\n%s", gathered.Text)
	}
}

// A capture's far floor verdicts ride its reading: a later gather of the
// same draft scores nothing again and sets the same candidates aside. An
// entry it hid that the draft then references is a rescue miss of the floor
// in the proof records, and no far entry's score reaches the ledger.
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
	store, proofs := local.NewMemoryRelevanceStore(), local.NewMemoryProofStore()
	scorer := &test.ScriptedScorer{Default: 0.1}
	world, session := newCaptureWorld(t, "grounding-floor",
		proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1, BriefAllBelow: 1}),
		proctest.WithRelevance(store), proctest.WithProofs(proofs), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: judge}),
		proctest.WithEmbedder(kappaEmbedder()), proctest.WithEntries(entries...))
	world.LLM.GatherQuery, world.LLM.GatherTerm = "omega gaps", "omega"
	instance := session.Start(t, "capture", nil).Instance
	draft := captureDraft()
	draft["body"] = "A tactical gap: the fixture observes something about omega."
	session.Report(t, instance, draft)

	first := session.Gather(t, instance)
	setAside := floorLine(first.Text)
	if strings.Count(setAside, "-s-tac-omg") != 6 || len(first.SummaryIDs) != 0 {
		t.Fatalf("every candidate rated at the floor should be set aside by it, none shown blind:\n%s", first.Text)
	}
	rescued := candidates[2]
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

	session.LogRead(t, "show", []string{rescued}, nil)
	draft["refs"] = append(draft["refs"].([]any), map[string]any{"id": rescued, "kind": "addresses", "desc": "bears on it"})
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
	verdict, ok := ledger.Reader(model.NewRelevancePair(entry.ID, rescued))
	if !ok || verdict.Code != model.ReaderReferenced || verdict.Audit {
		t.Fatalf("the rescued entry should be stored as a plain reference, got %+v, %v", verdict, ok)
	}
	proofSegments, err := proofs.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	records, err := relevance.FoldProofs(proofSegments)
	if err != nil {
		t.Fatal(err)
	}
	want := model.Miss{Capture: entry.ID, Hidden: rescued, Rule: model.RuleFarFloor, Detector: model.DetectRescue, Judge: judge}
	if len(records.Misses) != 1 || records.Misses[0].At.IsZero() {
		t.Fatalf("misses %+v: the rescue should be the one miss recorded", records.Misses)
	}
	if got := records.Misses[0]; got.Capture != want.Capture || got.Hidden != want.Hidden || got.Rule != want.Rule || got.Detector != want.Detector || got.Judge != want.Judge {
		t.Fatalf("miss %+v: the reference to an entry the floor hid should be a rescue miss of the floor", got)
	}
	if len(records.Tallies) != 1 || len(records.Tallies[0].Rules) != 2 || len(records.Tallies[0].Rules[1].Hidden) != 6 || records.Tallies[0].Rules[1].Step != model.StepNarrowed {
		t.Fatalf("tallies %+v: the capture's check should be tallied with the floor's six set-asides at the narrowed step", records.Tallies)
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
}

// A verdict the reading holds gives way to what the floor never overrides: a
// reference the draft adds next to the candidate, and a floor that misses
// from three captures took down to compute-only since.
func TestCaptureGrounding_HeldFarFloorVerdictsYieldToALaterReferenceAndAStepDown(t *testing.T) {
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
		after func(t *testing.T, proofs *local.MemoryProofStore, session *proctest.Session, instance string, draft map[string]any, target string)
		check func(t *testing.T, second sdd.GatherResult, neighbour string, other [2]string)
	}{
		{
			name: "a reference the draft adds",
			after: func(t *testing.T, _ *local.MemoryProofStore, session *proctest.Session, instance string, draft map[string]any, target string) {
				session.LogRead(t, "show", []string{target}, nil)
				draft["refs"] = append(draft["refs"].([]any), map[string]any{"id": target, "kind": "addresses", "desc": "bears on it"})
				session.Report(t, instance, draft)
			},
			check: func(t *testing.T, second sdd.GatherResult, neighbour string, other [2]string) {
				setAside := floorLine(second.Text)
				if strings.Contains(setAside, neighbour) || !slices.Contains(second.SummaryIDs, neighbour) {
					t.Fatalf("the candidate next to the new reference should return to the lines to judge:\n%s", second.Text)
				}
				if !strings.Contains(setAside, other[0]) || !strings.Contains(setAside, other[1]) {
					t.Fatalf("a candidate far from the new reference should keep its verdict:\n%s", second.Text)
				}
			},
		},
		{
			name: "a floor stepped down to compute-only",
			after: func(t *testing.T, proofs *local.MemoryProofStore, _ *proctest.Session, _ string, _ map[string]any, _ string) {
				var records model.ProofRecords
				for i := range model.MissesPerStep {
					records.Misses = append(records.Misses, model.Miss{Capture: fmt.Sprintf("20260301-10000%d-s-tac-cap", i), Hidden: "20260101-100000-s-tac-hid", Rule: model.RuleFarFloor, Detector: model.DetectRescue, Judge: judge, At: time.Date(2026, 3, 1, 10, 0, i, 0, time.UTC)})
				}
				segment, err := relevance.EncodeProofs(records)
				if err != nil {
					t.Fatal(err)
				}
				if err := proofs.Append(t.Context(), segment); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, second sdd.GatherResult, _ string, _ [2]string) {
				if floorLine(second.Text) != "" || len(second.SummaryIDs) != 2*len(pairs) {
					t.Fatalf("a floor at compute-only should set nothing aside, held verdicts included:\n%s", second.Text)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, proofs := local.NewMemoryRelevanceStore(), local.NewMemoryProofStore()
			scorer := &test.ScriptedScorer{Default: 0.1}
			world, session := newCaptureWorld(t, "grounding-floor-held",
				proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1, BriefAllBelow: 1}),
				proctest.WithRelevance(store), proctest.WithProofs(proofs), proctest.WithPrefilter(sdd.Prefilter{Scorer: scorer, Judge: judge}),
				proctest.WithEmbedder(kappaEmbedder()), proctest.WithEntries(entries...))
			world.LLM.GatherQuery, world.LLM.GatherTerm = "omega gaps", "omega"
			instance := session.Start(t, "capture", nil).Instance
			draft := captureDraft()
			draft["body"] = "A tactical gap: the fixture observes something about omega."
			session.Report(t, instance, draft)

			first := session.Gather(t, instance)
			if strings.Count(floorLine(first.Text), "-s-tac-omg") != 2*len(pairs) || len(first.SummaryIDs) != 0 {
				t.Fatalf("every candidate rated at the floor should be set aside by it:\n%s", first.Text)
			}
			scored := len(scorer.Scored())
			tc.after(t, proofs, session, instance, draft, pairs[0][0])
			second := session.Gather(t, instance)
			if len(scorer.Scored()) != scored {
				t.Fatalf("the second gather scored %d more lines: the reading's scores should be reused", len(scorer.Scored())-scored)
			}
			tc.check(t, second, pairs[0][1], pairs[1])
		})
	}
}

// An entry the check hid that the person names in their own words at
// playback is a person miss of the rule that hid it.
func TestCaptureGrounding_AHiddenEntryThePersonNamesIsAMiss(t *testing.T) {
	const judge = "test:scripted"
	var entries []*model.Entry
	for i := range 4 {
		text := fmt.Sprintf("An omega gap number %d.", i)
		entries = append(entries, &model.Entry{ID: fmt.Sprintf("20260201-1000%02d-s-tac-om%d", i, i), Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Summary: text, Content: text})
	}
	for i := range 3 {
		text := fmt.Sprintf("A far gap number %d.", i)
		entries = append(entries, &model.Entry{ID: fmt.Sprintf("20260202-1000%02d-s-tac-far", i), Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Summary: text, Content: text})
	}
	proofs := local.NewMemoryProofStore()
	world, session := newCaptureWorld(t, "grounding-person",
		proctest.WithGrounding(sdd.GroundingPolicy{Enforce: true, Prefilter: true, PrefilterFrom: 1, BriefAllBelow: 1}),
		proctest.WithRelevance(local.NewMemoryRelevanceStore()), proctest.WithProofs(proofs), proctest.WithPrefilter(sdd.Prefilter{Scorer: &test.ScriptedScorer{Default: 0.1}, Judge: judge}),
		proctest.WithEmbedder(kappaEmbedder()), proctest.WithEntries(entries...))
	world.LLM.GatherQuery, world.LLM.GatherTerm = "omega gaps", "omega"
	instance := session.Start(t, "capture", nil).Instance
	draft := captureDraft()
	draft["body"] = "A tactical gap: the fixture observes something about omega."
	session.Report(t, instance, draft)
	if gathered := session.Gather(t, instance); !strings.Contains(floorLine(gathered.Text), "s-tac-om1") {
		t.Fatalf("the floor should hide the omega gaps:\n%s", gathered.Text)
	}
	proctest.RequireStep(t, session.Report(t, instance, draft), "playback")
	serve := session.Answer(t, instance, "playback", "confirm", nil, "capture it, though om1 is 20260201-100001-s-tac-om1, check it later")
	proctest.RequireStep(t, serve, "verifySummary")
	serve = session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	entryID, _ := serve.Produced["entryId"].(string)

	segments, err := proofs.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	records, err := relevance.FoldProofs(segments)
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Misses) != 1 {
		t.Fatalf("misses %+v: the named hidden entry should be the one miss", records.Misses)
	}
	if miss := records.Misses[0]; miss.Capture != entryID || miss.Hidden != "20260201-100001-s-tac-om1" || miss.Rule != model.RuleFarFloor || miss.Detector != model.DetectPerson {
		t.Fatalf("miss %+v: the person named an entry the floor hid", miss)
	}
}
