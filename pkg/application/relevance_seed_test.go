package application_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

func TestSeedReplayLabels_AppendsReplayedReferencesOnce(t *testing.T) {
	const (
		landed  = "20261109-213250-d-tac-bve"
		first   = "20260416-150852-d-tac-s2g"
		second  = "20260416-171054-s-tac-kvb"
		labeled = "20260417-091917-d-tac-q5p"
	)
	store := localadapter.NewMemoryRelevanceStore()
	dismissed := model.ReaderVerdict{Code: model.ReaderDismissed, Reason: 1}
	if err := store.Append(t.Context(), relevance.Encode(model.RelevanceBatch{Readers: []model.ReaderRecord{{Pair: model.NewRelevancePair(landed, labeled), Verdict: dismissed}}})); err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"a":"` + landed + `","b":"` + first + `"}`,
		`{"a":"` + second + `","b":"` + landed + `"}`,
		`{"a":"` + first + `","b":"` + landed + `"}`,
		``,
		`{"a":"` + landed + `","b":"` + labeled + `"}`,
	}, "\n")

	result, err := sdd.SeedReplayLabels(t.Context(), store, strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != 2 || result.Skipped != 2 {
		t.Fatalf("result %+v: the repeated line and the labelled pair should be skipped", result)
	}
	again, err := sdd.SeedReplayLabels(t.Context(), store, strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if again.Added != 0 || again.Skipped != 4 {
		t.Fatalf("result %+v: a second pass over one file should add nothing", again)
	}
	segments, err := store.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 {
		t.Fatalf("%d segments: a pass adding nothing should append no segment", len(segments))
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{first, second} {
		got, _ := ledger.Reader(model.NewRelevancePair(landed, id))
		if got.Code != model.ReaderReferenced || got.Source != model.SourceReplay || got.Audit {
			t.Fatalf("pair with %s = %+v: want a replayed reference outside the audit", id, got)
		}
	}
	if got, _ := ledger.Reader(model.NewRelevancePair(landed, labeled)); got != dismissed {
		t.Fatalf("the labelled pair = %+v: seeding should leave it as it was", got)
	}
}

func TestSeedReplayLabels_RefusesAMalformedLine(t *testing.T) {
	for _, line := range []string{
		`{"a":"20261109-213250-d-tac-bve"}`,
		`{"a":"20261109-213250-d-tac-bve","b":"not an id"}`,
		`{"a":"20261109-213250-d-tac-bve","b":"20261109-213250-d-tac-bve"}`,
		`{"a":"20261109-213250-d-tac-bve","b":"20260416-150852-d-tac-s2g","code":"opened"}`,
	} {
		store := localadapter.NewMemoryRelevanceStore()
		_, err := sdd.SeedReplayLabels(t.Context(), store, strings.NewReader(`{"a":"20260416-171054-s-tac-kvb","b":"20260416-150852-d-tac-s2g"}`+"\n"+line+"\n"))
		if err == nil || !strings.Contains(err.Error(), "line 2") {
			t.Fatalf("%s: err = %v; want a refusal naming the line", line, err)
		}
		if segments, _ := store.Segments(t.Context()); len(segments) != 0 {
			t.Fatalf("%s: a refused file should append nothing", line)
		}
	}
}

// Seeding from the graph's refs labels each referenced pair once, and one
// rescore pass, a request per referencing entry, gives the judge the scores
// its calibration needs.
func TestSeedRefLabels_SeedsTheGraphsRefsForTheJudgeToCalibrate(t *testing.T) {
	f := newGatherFixture(t)
	subject := "20260201-000000-d-tac-sub"
	var refs strings.Builder
	refs.WriteString("intent: pending\nrefs:\n")
	for i := range 189 {
		id := fmt.Sprintf("20260101-%02d%02d00-s-tac-ref", i/60, i%60)
		f.write(t, id, "gap", fmt.Sprintf("Referenced gap %d.", i), "")
		fmt.Fprintf(&refs, "    - id: %s\n      kind: grounded-in\n      desc: a referenced gap\n", id)
	}
	f.write(t, subject, "directive", "The subject decision.", refs.String())
	f.relevance = localadapter.NewMemoryRelevanceStore()
	scorer := &scriptedScorer{scores: map[string]float64{}}
	f.prefilter = sdd.Prefilter{Scorer: scorer, Judge: gatherJudge}
	application := f.app(t, prefilterPolicy(1), nil)
	identity := sdd.RequestIdentity{Subject: "tester"}

	seeded, err := application.SeedRefLabels(t.Context(), identity, gatherProject)
	if err != nil {
		t.Fatal(err)
	}
	if seeded.Added != 189 || seeded.Skipped != 0 {
		t.Fatalf("seed %+v: each referenced pair should be labelled once", seeded)
	}
	scored, err := application.RescoreRelevance(t.Context(), identity, gatherProject)
	if err != nil {
		t.Fatal(err)
	}
	if scored.Scored != 189 || scored.Requests != 1 || scorer.calls() != 1 {
		t.Fatalf("rescore %+v after %d calls: one request should score the referencing entry's pairs", scored, scorer.calls())
	}
	state, err := application.PrefilterState(t.Context(), identity, gatherProject)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Calibrated || state.Judge != gatherJudge {
		t.Fatalf("state %+v: the seeded and scored labels should calibrate the judge", state)
	}
	again, err := application.SeedRefLabels(t.Context(), identity, gatherProject)
	if err != nil {
		t.Fatal(err)
	}
	if again.Added != 0 || again.Skipped != 189 {
		t.Fatalf("seed %+v: a second pass should add nothing", again)
	}
}

func TestSeedRefLabels_NeedsAJudge(t *testing.T) {
	f := newGatherFixture(t)
	f.relevance = localadapter.NewMemoryRelevanceStore()
	application := f.app(t, prefilterPolicy(1), nil)
	_, err := application.SeedRefLabels(t.Context(), sdd.RequestIdentity{Subject: "tester"}, gatherProject)
	if err == nil || !strings.Contains(err.Error(), "no prefilter judge") {
		t.Fatalf("seeding without a judge = %v, should be refused: its labels could never be scored", err)
	}
}
