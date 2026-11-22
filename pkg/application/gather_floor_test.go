package application_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	llmrelevance "github.com/networkteam/sdd/pkg/llm/relevance"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// The far floor fixture: a draft on topic cli/ux that references
// floorReference and searches for "omega". Three entries are far from it.
// The search with the draft's text lists only filler gaps, none of the
// fixture's own entries, so each of those is found by the reads the test
// names.
const (
	floorReference = "20260103-100000-d-tac-ref"
	floorPlain     = "20260103-100001-s-tac-pln"
	floorHigh      = "20260103-100002-s-tac-hgh"
	floorTwoReads  = "20260103-100003-s-tac-two"
	floorNear      = "20260103-100004-s-tac-nea"
	floorTopic     = "20260103-100005-s-tac-top"
	floorFarLow    = "20260103-100006-s-tac-fa1"
	floorFarMid    = "20260103-100007-s-tac-fa2"
	floorFarHigh   = "20260103-100008-s-tac-fa3"
)

var floorFar = []string{floorFarLow, floorFarMid, floorFarHigh}

// gatherSearchHits is how many hits one search of a gather lists.
const gatherSearchHits = 20

// floorFixture writes the fixture graph, with far of its three far
// entries. The scorer rates the far entries 0.1, 0.3 and 0.5, so the careful
// line of a call is 0.1 and its median line 0.3.
func floorFixture(t *testing.T, scorer *scriptedScorer, far int) *gatherFixture {
	t.Helper()
	f := newGatherFixture(t)
	for i := range gatherSearchHits {
		f.write(t, fmt.Sprintf("20260104-1000%02d-s-tac-f%02d", i, i), "gap", fmt.Sprintf("A filler gap number %d.", i), "")
	}
	f.unsearched = true
	f.write(t, floorReference, "directive", "The directive the draft references.", "intent: pending\n")
	f.write(t, floorPlain, "gap", "An omega gap one search finds.", "")
	f.write(t, floorHigh, "gap", "An omega gap the judge rates high.", "")
	f.write(t, floorTwoReads, "gap", "An omega gap two reads find.", "topics:\n  - cli/ux\n")
	f.write(t, floorNear, "gap", "An omega gap next to the reference.", "refs:\n  - "+floorReference+"\n")
	f.write(t, floorTopic, "gap", "A gap that only shares the topic.", "topics:\n  - cli/ux\n")
	for i, id := range floorFar[:far] {
		extra := ""
		if i == 1 {
			extra = "topics:\n  - garden/beds\n"
		}
		f.write(t, id, "gap", fmt.Sprintf("A far gap number %d.", i+1), extra)
	}
	scorer.scores = map[string]float64{
		floorFarLow: 0.1, floorFarMid: 0.3, floorFarHigh: 0.5,
		floorPlain: 0.1, floorHigh: 0.9, floorTwoReads: 0, floorNear: 0, floorTopic: 0.3,
	}
	f.prefilter = sdd.Prefilter{Scorer: scorer, Judge: gatherJudge}
	return f
}

func floorPolicy() sdd.GroundingPolicy {
	return sdd.GroundingPolicy{Prefilter: true, PrefilterFrom: 1}
}

func floorRequest() sdd.GatherRequest {
	return sdd.GatherRequest{
		Draft: "A draft about omega.", Kind: "gap", Topics: []string{"cli/ux"}, Terms: []string{"omega"},
		References: []string{floorReference}, Answered: []string{floorReference},
	}
}

// farIn returns the far entries a judge call carried.
func farIn(request llmrelevance.Request) []string {
	var far []string
	for _, candidate := range request.Candidates {
		if slices.Contains(floorFar, candidate.ID) {
			far = append(far, candidate.ID)
		}
	}
	slices.Sort(far)
	return far
}

// floorProofs holds 189 replayed checks by gatherJudge whose needed entry
// scores above the median line of its call, a full proof of the far floor,
// and misses of the floor from as many captures. The same checks prove the
// threshold they calibrate, so misses from six captures hold it at
// compute-only, leaving the floor alone to set aside.
// and misses of the floor from as many captures.
func floorProofs(t *testing.T, misses int) *localadapter.MemoryProofStore {
	t.Helper()
	var records model.ProofRecords
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 189 {
		records.Replays = append(records.Replays, model.ReplayCheck{
			Subject: fmt.Sprintf("20250701-%06d-d-tac-rpl", i), Judge: gatherJudge, At: at, GraphSize: 100000,
			Candidates: []model.ReplayCandidate{{ID: fmt.Sprintf("20250102-%06d-s-tac-ned", i), Score: 200, Far: true, Lowest: 20, Median: 40, Needed: true}},
		})
	}
	for i := range 2 * model.MissesPerStep {
		records.Misses = append(records.Misses, model.Miss{Capture: fmt.Sprintf("20260302-%06d-s-tac-cap", i), Hidden: floorPlain, Rule: model.RuleThreshold, Detector: model.DetectRescue, Judge: gatherJudge, At: at.Add(time.Hour)})
	}
	for i := range misses {
		records.Misses = append(records.Misses, model.Miss{Capture: fmt.Sprintf("20260301-%06d-s-tac-cap", i), Hidden: floorPlain, Rule: model.RuleFarFloor, Detector: model.DetectRescue, Judge: gatherJudge, At: at.Add(time.Hour)})
	}
	segment, err := relevance.EncodeProofs(records)
	if err != nil {
		t.Fatal(err)
	}
	store := localadapter.NewMemoryProofStore()
	if err := store.Append(t.Context(), segment); err != nil {
		t.Fatal(err)
	}
	return store
}

// Every judge call carries the three far entries, and without a proof a
// candidate rated no higher than the lowest of them is set aside in a group
// of its own, with and without thresholds. A candidate next to the draft's
// reference and one two reads found stay, whatever their score.
func TestGather_FarFloorSetsAsideWhatTheJudgeRatesNoHigherThanFarEntries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		calibrated bool
	}{{name: "without thresholds"}, {name: "with thresholds", calibrated: true}} {
		t.Run(tc.name, func(t *testing.T) {
			scorer := &scriptedScorer{maxBatch: 5}
			f := floorFixture(t, scorer, 3)
			if tc.calibrated {
				f.relevance, f.proofs = calibratedLedger(t), provenThreshold(t)
			}
			result, err := gather(t, f.app(t, floorPolicy(), nil), floorRequest())
			if err != nil {
				t.Fatal(err)
			}
			if len(scorer.requests) < 3 {
				t.Fatalf("%d calls: five candidates and more should take calls of two beside the far entries", len(scorer.requests))
			}
			for _, request := range scorer.requests {
				if len(request.Candidates) > 5 || !slices.Equal(farIn(request), floorFar) {
					t.Fatalf("a call of %d lines with far entries %v: every call should carry the three far entries within the scorer's batch", len(request.Candidates), farIn(request))
				}
				for _, candidate := range request.Candidates {
					if candidate.ID == floorFarMid && candidate.Line != "[signal/gap] A far gap number 2." {
						t.Fatalf("far line %q should have the form of a candidate's line", candidate.Line)
					}
				}
			}
			for _, id := range floorFar {
				if strings.Contains(result.Text, id) {
					t.Fatalf("far entry %s should reach no part of the serve:\n%s", id, result.Text)
				}
			}
			floored := setAsideCount(result.Text, model.SetAsideFloor)
			if slices.Contains(result.SummaryIDs, floorPlain) {
				t.Fatalf("a candidate rated no higher than the lowest far score should be set aside by the floor:\n%s", result.Text)
			}
			if floored != 1 {
				t.Fatalf("%d below the floor: without a proof the careful line holds, so the one rated no higher than the lowest far score, and only it, falls below:\n%s", floored, result.Text)
			}
			for _, id := range []string{floorTwoReads, floorNear} {
				if !tc.calibrated && !slices.Contains(result.SummaryIDs, id) {
					t.Fatalf("%s should stay a line:\n%s", id, result.Text)
				}
			}
			if line := lineOf(result.Text, floorHigh); line == "" || strings.HasPrefix(line[1:], " ?") {
				t.Fatalf("a candidate rated above the floor should stay a scored line:\n%s", result.Text)
			}
			if !strings.Contains(result.Text, fmt.Sprintf("; the far floor set aside %d\n", floored)) {
				t.Fatalf("the prefilter note should name the %d candidates the floor set aside:\n%s", floored, result.Text)
			}
		})
	}
}

// A reading that grounds an existing entry reads that entry's own
// references: a candidate next to one of them never falls below the floor.
func TestGather_FarFloorReadsASubjectsOwnReferences(t *testing.T) {
	const subject = "20260103-100009-d-tac-sub"
	scorer := &scriptedScorer{}
	f := floorFixture(t, scorer, 3)
	f.write(t, subject, "directive", "A subject directive about omega.", "intent: pending\ntopics:\n  - cli/ux\nrefs:\n  - "+floorReference+"\n")
	result, err := gather(t, f.app(t, floorPolicy(), nil), sdd.GatherRequest{Subject: subject, Terms: []string{"omega"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(scorer.requests) != 1 || !slices.Equal(farIn(scorer.requests[0]), floorFar) {
		t.Fatalf("%d calls: the subject's topic and reference should give the reading a floor", len(scorer.requests))
	}
	if !slices.Contains(result.SummaryIDs, floorNear) || slices.Contains(result.SummaryIDs, floorPlain) || setAsideCount(result.Text, model.SetAsideFloor) != 1 {
		t.Fatalf("the candidate next to the subject's reference should stay above the floor, which sets aside the low-rated one:\n%s", result.Text)
	}
}

// A call the judge breaks on is scored again in halves, and each half
// carries the far entries too.
func TestGather_FarFloorRidesEveryPartOfAHalvedCall(t *testing.T) {
	inner := &scriptedScorer{maxBatch: 100, short: true, limit: 5}
	f := floorFixture(t, inner, 3)
	f.prefilter.Scorer = llmrelevance.Halving(inner, &llmrelevance.MemoryBatchSizes{}, gatherJudge)
	result, err := gather(t, f.app(t, floorPolicy(), nil), floorRequest())
	if err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, request := range inner.requests {
		lines = append(lines, len(request.Candidates))
		if !slices.Equal(farIn(request), floorFar) {
			t.Fatalf("a call of %d lines with far entries %v: every part should carry the three far entries", len(request.Candidates), farIn(request))
		}
	}
	if len(lines) < 2 || lines[0] <= 5 || slices.ContainsFunc(lines[1:], func(n int) bool { return n == lines[0] }) {
		t.Fatalf("calls of %v lines: the broken first call should be scored again in smaller parts", lines)
	}
	if setAsideCount(result.Text, model.SetAsideFloor) == 0 || !strings.Contains(result.Text, "prefilter: ok: ") {
		t.Fatalf("the halved pass should score every candidate and set aside below the floor:\n%s", result.Text)
	}
}

// Without a floor the gather is the one a judge without far entries runs.
func TestGather_FarFloorNeedsFarEntriesADraftToBeFarFromAndRoomInACall(t *testing.T) {
	for _, tc := range []struct {
		name     string
		far      int
		maxBatch int
		request  func(*sdd.GatherRequest)
	}{
		{name: "two far entries", far: 2},
		{name: "a draft with neither topic nor reference", far: 3, request: func(r *sdd.GatherRequest) { r.Topics, r.References = nil, nil }},
		{name: "a scorer whose call the far entries fill", far: 3, maxBatch: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scorer := &scriptedScorer{maxBatch: tc.maxBatch}
			f := floorFixture(t, scorer, tc.far)
			request := floorRequest()
			if tc.request != nil {
				tc.request(&request)
			}
			result, err := gather(t, f.app(t, floorPolicy(), nil), request)
			if err != nil {
				t.Fatal(err)
			}
			if scorer.calls() == 0 {
				t.Fatal("the prefilter should still score the candidates")
			}
			for _, call := range scorer.requests {
				if far := farIn(call); len(far) > 0 {
					t.Fatalf("a call carried far entries %v", far)
				}
			}
			if strings.Contains(result.Text, model.SetAsideFloor) || strings.Contains(result.Text, "far floor") {
				t.Fatalf("no floor should set aside or be named:\n%s", result.Text)
			}
			if !slices.Contains(result.SummaryIDs, floorPlain) {
				t.Fatalf("a low-rated candidate should stay to judge:\n%s", result.Text)
			}
		})
	}
}

// The far floor's step sets its form: the median form only at the full
// step, the careful form below it, and at compute-only the calls still
// carry the far entries but nothing is set aside.
func TestGather_FarFloorTakesTheFormItsStepAllows(t *testing.T) {
	for _, tc := range []struct {
		name    string
		proofs  func(*testing.T) *localadapter.MemoryProofStore
		floored []string
		kept    []string
		note    string
	}{
		{name: "full", proofs: func(t *testing.T) *localadapter.MemoryProofStore { return floorProofs(t, 0) }, floored: []string{floorPlain, floorTopic}, note: "; the far floor set aside 2;"},
		{name: "narrowed by misses from three captures", proofs: func(t *testing.T) *localadapter.MemoryProofStore { return floorProofs(t, 3) }, floored: []string{floorPlain}, kept: []string{floorTopic}, note: "; the far floor set aside 1;"},
		{name: "compute-only by misses from six captures", proofs: func(t *testing.T) *localadapter.MemoryProofStore { return floorProofs(t, 6) }, kept: []string{floorPlain, floorTopic}, note: "; the far-floor holds no step to hide at and would set aside 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scorer := &scriptedScorer{maxBatch: 5}
			f := floorFixture(t, scorer, 3)
			f.proofs = tc.proofs(t)
			result, err := gather(t, f.app(t, floorPolicy(), &countingEmbeddings{}), floorRequest())
			if err != nil {
				t.Fatal(err)
			}
			for _, request := range scorer.requests {
				if !slices.Equal(farIn(request), floorFar) {
					t.Fatal("every call should carry the far entries, at every step")
				}
			}
			if setAsideCount(result.Text, model.SetAsideFloor) != len(tc.floored) {
				t.Fatalf("%v should fall below the floor at this step, and only they:\n%s", tc.floored, result.Text)
			}
			for _, id := range tc.floored {
				if slices.Contains(result.SummaryIDs, id) {
					t.Fatalf("%s should fall below the floor at this step:\n%s", id, result.Text)
				}
			}
			for _, id := range tc.kept {
				if !slices.Contains(result.SummaryIDs, id) {
					t.Fatalf("%s should stay to judge at this step:\n%s", id, result.Text)
				}
			}
			if !strings.Contains(result.Text, tc.note) {
				t.Fatalf("the prefilter note should hold %q:\n%s", tc.note, result.Text)
			}
		})
	}
}

func TestGroundingStatus_ReportsTheHidingRules(t *testing.T) {
	for _, tc := range []struct {
		name   string
		proofs func(*testing.T) *localadapter.MemoryProofStore
		want   []string
	}{
		{name: "without a proof", want: []string{"threshold: compute-only · no proof yet", "far-floor: narrowed · no proof yet; careful form from the first check"}},
		{name: "with proofs", proofs: func(t *testing.T) *localadapter.MemoryProofStore { return floorProofs(t, 3) }, want: []string{
			"far-floor: narrowed · proof: 0 of 189 needed entries hidden in 189 replays, bound 0.980, graph of 100,000; 3 misses from 3 captures since its latest replay",
			"misses: 3 rescue, 0 verifier, 0 person, 0 scan (no planted miss yet)",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := prefilterFixture(t, &scriptedScorer{})
			f.relevance = calibratedLedger(t)
			if tc.proofs != nil {
				f.proofs = tc.proofs(t)
			}
			application := f.app(t, prefilterPolicy(1), nil)
			identity := sdd.RequestIdentity{Subject: "tester"}
			for _, verbose := range []bool{false, true} {
				status, err := application.GroundingStatus(t.Context(), identity, gatherProject, sdd.GroundingStatusRequest{Verbose: verbose})
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range tc.want {
					if !strings.Contains(status.Text, want) {
						t.Errorf("the status lacks %q:\n%s", want, status.Text)
					}
				}
			}
		})
	}
}
