package relevance_test

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// callScorer scores a far candidate at a thousandth of the size of the call
// it rides, so a floor names its call, and every other candidate at half. A
// call of more than limit candidates is answered one score short.
type callScorer struct {
	mu       sync.Mutex
	maxBatch int
	limit    int
	requests []relevance.Request
}

func (s *callScorer) Direction() relevance.Direction { return relevance.Both }
func (s *callScorer) MaxBatch() int                  { return s.maxBatch }
func (s *callScorer) MaxInputTokens() int            { return 0 }
func (s *callScorer) MaxPairTokens() int             { return 0 }

func (s *callScorer) Score(_ context.Context, request relevance.Request) (relevance.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, request)
	var result relevance.Result
	for _, candidate := range request.Candidates {
		score := 0.5
		if strings.HasPrefix(candidate.ID, "far-") {
			score = float64(len(request.Candidates)) / 1000
		}
		result.Scores = append(result.Scores, score)
	}
	if s.limit > 0 && len(request.Candidates) > s.limit {
		result.Scores = result.Scores[1:]
	}
	return result, nil
}

func farCandidates(lengths ...int) []relevance.Candidate {
	out := make([]relevance.Candidate, len(lengths))
	for i, length := range lengths {
		out[i] = relevance.Candidate{ID: "far-" + string(rune('x'+i)), Text: strings.Repeat("f", length)}
	}
	return out
}

func idsOf(candidates []relevance.Candidate) []string {
	ids := make([]string, len(candidates))
	for i, candidate := range candidates {
		ids[i] = candidate.ID
	}
	return ids
}

func TestPlanWithFar(t *testing.T) {
	candidates := make([]relevance.Candidate, 10)
	for i := range candidates {
		candidates[i] = relevance.Candidate{ID: string(rune('a' + i)), Text: strings.Repeat("c", 30)}
	}
	for _, tc := range []struct {
		name     string
		scorer   *fixedScorer
		far      []relevance.Candidate
		ok       bool
		sizes    []int
		oversize int
	}{
		{name: "the far candidates take their room of each call", scorer: &fixedScorer{maxBatch: 7}, far: farCandidates(3, 3, 3), ok: true, sizes: []int{4, 4, 2}},
		{name: "an unbounded scorer takes one call", scorer: &fixedScorer{}, far: farCandidates(3, 3, 3), ok: true, sizes: []int{10}},
		{name: "a batch with room for one candidate", scorer: &fixedScorer{maxBatch: 4}, far: farCandidates(3, 3, 3), ok: true, sizes: []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1}},
		{name: "a batch the far candidates fill has no plan", scorer: &fixedScorer{maxBatch: 3}, far: farCandidates(3, 3, 3)},
		{name: "the far candidates count against the tokens of each call", scorer: &fixedScorer{maxTokens: 46}, far: farCandidates(30, 30, 30), ok: true, sizes: []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1}},
		{name: "far candidates past the tokens of a call have no plan", scorer: &fixedScorer{maxTokens: 20}, far: farCandidates(30, 30, 30)},
		{name: "a far candidate past the pair limit has no plan", scorer: &fixedScorer{maxPair: 12}, far: farCandidates(3, 60, 3)},
		{name: "a candidate with no room beside the far ones is left out", scorer: &fixedScorer{maxTokens: 36}, far: farCandidates(30, 30, 30), ok: true, oversize: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batches, oversize, ok := relevance.PlanWithFar(tc.scorer, "subject", candidates, tc.far)
			if ok != tc.ok {
				t.Fatalf("ok = %v: a plan exists only where a call holds the far candidates and one more", ok)
			}
			var sizes []int
			for _, batch := range batches {
				sizes = append(sizes, len(batch))
			}
			if !slices.Equal(sizes, tc.sizes) || len(oversize) != tc.oversize {
				t.Fatalf("batches %v, %d oversize: each call should leave the far candidates their room", sizes, len(oversize))
			}
		})
	}
}

func TestPlan_KeepsItsCallsWithoutFarCandidates(t *testing.T) {
	scorer := &fixedScorer{maxBatch: 4, maxTokens: 40}
	candidates := manyCandidates(11)
	batches, oversize := relevance.Plan(scorer, "subject", candidates)
	planned, plannedOversize, ok := relevance.PlanWithFar(scorer, "subject", candidates, nil)
	if !ok || !reflect.DeepEqual(batches, planned) || !slices.Equal(oversize, plannedOversize) {
		t.Fatalf("plan %v and %v: without far candidates both plans should be one", batches, planned)
	}
}

func TestScoreWithFar_MixesTheFarCandidatesIntoTheCall(t *testing.T) {
	scorer := &fixedScorer{}
	request := relevance.Request{Subject: "subject", Candidates: manyCandidates(6)}
	far := farCandidates(10, 50, 30)
	result, err := relevance.ScoreWithFar(t.Context(), scorer, request, far)
	if err != nil {
		t.Fatal(err)
	}
	if len(scorer.requests) != 1 || len(scorer.requests[0].Candidates) != 9 {
		t.Fatalf("%d calls: one call should carry the candidates and the far ones", len(scorer.requests))
	}
	sent := idsOf(scorer.requests[0].Candidates)
	for _, candidate := range far {
		if !slices.Contains(sent, candidate.ID) {
			t.Fatalf("call %v lacks far candidate %s", sent, candidate.ID)
		}
	}
	kept := slices.DeleteFunc(slices.Clone(sent), func(id string) bool { return strings.HasPrefix(id, "far-") })
	if !slices.Equal(kept, idsOf(request.Candidates)) {
		t.Fatalf("call %v: the candidates should keep their order around the far ones", sent)
	}
	if slices.Equal(sent[len(sent)-3:], idsOf(far)) {
		t.Fatalf("call %v: the far candidates should not trail the list", sent)
	}
	want := make([]float64, 6)
	for i, candidate := range request.Candidates {
		want[i] = float64(len(candidate.Text)) / 100
	}
	if !slices.Equal(result.Scores, want) {
		t.Fatalf("scores %v: the result should hold the candidates' scores alone, in request order", result.Scores)
	}
	if !slices.Equal(result.Far, slices.Repeat([]relevance.FarLine{{Lowest: 0.1, Median: 0.3}}, 6)) {
		t.Fatalf("lines %v: every candidate should carry the lowest and the median of the far scores", result.Far)
	}
	_, err = relevance.ScoreWithFar(t.Context(), scorer, request, far)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(idsOf(scorer.requests[1].Candidates), sent) {
		t.Fatal("a repeated request should place the far candidates alike")
	}
}

func TestScoreWithFar_WithoutFarCandidatesIsThePlainCall(t *testing.T) {
	scorer := &fixedScorer{}
	request := relevance.Request{Subject: "subject", Candidates: manyCandidates(4)}
	result, err := relevance.ScoreWithFar(t.Context(), scorer, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scorer.requests[0], request) || result.Far != nil || len(result.Scores) != 4 {
		t.Fatalf("result %+v: without far candidates the request should pass unchanged and hold no floor", result)
	}
}

// A call that breaks is scored again in halves, each half with every far
// candidate, and a candidate's line is the one its own call measured.
func TestScoreWithFar_HalvingCarriesTheFarCandidatesInEveryPart(t *testing.T) {
	inner := &callScorer{maxBatch: 13, limit: 8}
	sizes := &relevance.MemoryBatchSizes{}
	scorer := relevance.Halving(inner, sizes, halvingKey)
	request := relevance.Request{Subject: "subject", Candidates: manyCandidates(10)}
	far := farCandidates(1, 1, 1)
	result, err := relevance.ScoreWithFar(t.Context(), scorer, request, far)
	if err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, call := range inner.requests {
		lines = append(lines, len(call.Candidates))
		sent := idsOf(call.Candidates)
		for _, candidate := range far {
			if !slices.Contains(sent, candidate.ID) {
				t.Fatalf("call %v lacks far candidate %s", sent, candidate.ID)
			}
		}
	}
	if !slices.Equal(lines, []int{13, 8, 8}) {
		t.Fatalf("calls of %v lines: the broken call should be scored again in two halves", lines)
	}
	if len(result.Scores) != 10 || len(result.Far) != 10 {
		t.Fatalf("%d scores, %d floors for 10 candidates", len(result.Scores), len(result.Far))
	}
	for i, line := range result.Far {
		if line.Lowest != 0.008 || line.Median != 0.008 {
			t.Fatalf("line %v of candidate %d: it should be the one its own call of 8 lines measured", line, i)
		}
	}
	held, ok := sizes.Size(halvingKey)
	if !ok || held != 8 {
		t.Fatalf("remembered size %d, %v: the half should count the far candidates it carries", held, ok)
	}
}

func TestScoreWithFar_HalvingSplitsARequestPastItsRememberedSize(t *testing.T) {
	inner := &callScorer{maxBatch: 100}
	scorer := relevance.Halving(inner, remembering(8), halvingKey)
	request := relevance.Request{Subject: "subject", Candidates: manyCandidates(12)}
	result, err := relevance.ScoreWithFar(t.Context(), scorer, request, farCandidates(1, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, call := range inner.requests {
		lines = append(lines, len(call.Candidates))
	}
	if !slices.Equal(lines, []int{8, 8, 5}) {
		t.Fatalf("calls of %v lines: no call should pass the remembered size, far candidates counted", lines)
	}
	if line := result.Far[0]; line.Lowest != 0.008 || result.Far[4] != line || result.Far[11].Lowest != 0.005 {
		t.Fatalf("lines %v: each candidate should carry the line of its own call", result.Far)
	}
}

// A call that fills the remembered size only with its far candidates
// counted is one that answered at that size, and doubles it.
func TestScoreWithFar_HalvingCountsTheFarCandidatesTowardsARaise(t *testing.T) {
	inner := &callScorer{maxBatch: 100}
	sizes := remembering(8)
	scorer := relevance.Halving(inner, sizes, halvingKey)
	_, err := relevance.ScoreWithFar(t.Context(), scorer, relevance.Request{Subject: "subject", Candidates: manyCandidates(6)}, farCandidates(1, 1, 1))
	if err != nil {
		t.Fatal(err)
	}
	held, _ := sizes.Size(halvingKey)
	if held != 16 {
		t.Fatalf("remembered size %d: a call of eight lines that answered should double the size", held)
	}
}

func TestScoreWithFar_RefusesAnAnswerShortOfAScore(t *testing.T) {
	scorer := &callScorer{limit: 2}
	_, err := relevance.ScoreWithFar(t.Context(), scorer, relevance.Request{Subject: "subject", Candidates: manyCandidates(3)}, farCandidates(1, 1, 1))
	if err == nil {
		t.Fatal("an answer one score short should fail the call: no score can be told from a far one")
	}
}
