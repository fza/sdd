package application_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

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
// entries. The scorer rates the far entries 0.1, 0.3 and 0.5, so the floor
// of a call is 0.3.
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
		floorPlain: 0.2, floorHigh: 0.9, floorTwoReads: 0, floorNear: 0, floorTopic: 0.3,
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

// floorMissLedger holds sixty audit labels of far floor set-asides scored
// by gatherJudge, two of them referenced: above the bar that turns the
// floor off.
func floorMissLedger(t *testing.T) *localadapter.MemoryRelevanceStore {
	t.Helper()
	var batch model.RelevanceBatch
	topicOnly, _ := model.VerdictReasonByte(model.VerdictTopicOnly)
	for i := range 60 {
		pair := model.NewRelevancePair(fmt.Sprintf("20250101-%06d-s-tac-aud", i), fmt.Sprintf("20250601-%06d-d-tac-sub", i))
		verdict := model.ReaderVerdict{Code: model.ReaderDismissed, Reason: topicOnly, Audit: true, Floor: true}
		if i < 2 {
			verdict = model.ReaderVerdict{Code: model.ReaderReferenced, Audit: true, Floor: true}
		}
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: verdict})
		batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: gatherJudge, Score: 5}})
	}
	store := localadapter.NewMemoryRelevanceStore()
	if err := store.Append(t.Context(), relevance.Encode(batch)); err != nil {
		t.Fatal(err)
	}
	return store
}

// Every judge call carries the three far entries, and a candidate rated no
// higher than their median is set aside in a group of its own, with and
// without thresholds. A candidate next to the draft's reference and one two
// reads found stay, whatever their score.
func TestGather_FarFloorSetsAsideWhatTheJudgeRatesNoHigherThanFarEntries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		calibrated bool
	}{{name: "without thresholds"}, {name: "with thresholds", calibrated: true}} {
		t.Run(tc.name, func(t *testing.T) {
			scorer := &scriptedScorer{maxBatch: 5}
			f := floorFixture(t, scorer, 3)
			if tc.calibrated {
				f.relevance = calibratedLedger(t)
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
			audited := 0
			for _, id := range []string{floorPlain, floorTopic} {
				if slices.Contains(result.SummaryIDs, id) {
					audited++
					if !strings.HasPrefix(lineOf(result.Text, id), "  ?") {
						t.Fatalf("%s, rated no higher than the floor, should show only as an audit line:\n%s", id, result.Text)
					}
				}
			}
			if floored+audited != 2 {
				t.Fatalf("%d below the floor: the two rated no higher than the floor, and only they, should be set aside by it, but for an audit line:\n%s", floored, result.Text)
			}
			for _, id := range []string{floorTwoReads, floorNear} {
				if !tc.calibrated && !slices.Contains(result.SummaryIDs, id) {
					t.Fatalf("%s should stay a line:\n%s", id, result.Text)
				}
			}
			if line := lineOf(result.Text, floorHigh); line == "" || strings.HasPrefix(line[1:], " ?") {
				t.Fatalf("a candidate rated above the floor should stay a scored line:\n%s", result.Text)
			}
			if floored == 0 || !strings.Contains(result.Text, fmt.Sprintf("; the far floor set aside %d\n", floored)) {
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
	if !slices.Contains(result.SummaryIDs, floorNear) || setAsideCount(result.Text, model.SetAsideFloor) != 1 {
		t.Fatalf("the candidate next to the subject's reference should stay above the floor, which sets aside the two low-rated ones but for an audit line:\n%s", result.Text)
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
		ledger   func(*testing.T) *localadapter.MemoryRelevanceStore
		request  func(*sdd.GatherRequest)
	}{
		{name: "two far entries", far: 2},
		{name: "a draft with neither topic nor reference", far: 3, request: func(r *sdd.GatherRequest) { r.Topics, r.References = nil, nil }},
		{name: "a scorer whose call the far entries fill", far: 3, maxBatch: 3},
		{name: "a judge whose floor showed too many misses", far: 3, ledger: floorMissLedger},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scorer := &scriptedScorer{maxBatch: tc.maxBatch}
			f := floorFixture(t, scorer, tc.far)
			if tc.ledger != nil {
				f.relevance = tc.ledger(t)
			}
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

func TestGroundingStatus_ReportsTheFarFloor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ledger  func(*testing.T) *localadapter.MemoryRelevanceStore
		verbose string
		summary string
	}{
		{name: "on", ledger: calibratedLedger, verbose: "far floor: on; 0 referenced of the latest 0 audit labels of its set-asides", summary: "floor:   on · "},
		{name: "off", ledger: floorMissLedger, verbose: "far floor: off, its audit showed too many misses; 2 referenced of the latest 60 audit labels of its set-asides", summary: "floor:   off, its audit showed too many misses · "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := prefilterFixture(t, &scriptedScorer{})
			f.relevance = tc.ledger(t)
			application := f.app(t, prefilterPolicy(1), nil)
			identity := sdd.RequestIdentity{Subject: "tester"}
			verbose, err := application.GroundingStatus(t.Context(), identity, gatherProject, sdd.GroundingStatusRequest{Verbose: true})
			if err != nil {
				t.Fatal(err)
			}
			summary, err := application.GroundingStatus(t.Context(), identity, gatherProject, sdd.GroundingStatusRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(verbose.Text, tc.verbose) || !strings.Contains(summary.Text, tc.summary) {
				t.Fatalf("the status should name the floor's state and counts:\n%s\n%s", verbose.Text, summary.Text)
			}
		})
	}
}
