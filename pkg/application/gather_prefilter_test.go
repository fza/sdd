package application_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	llmrelevance "github.com/networkteam/sdd/pkg/llm/relevance"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

const gatherJudge = "test:scripted"

// scriptedScorer scores candidates from a table, 0.5 for any other, and
// fails the candidates it is told to.
type scriptedScorer struct {
	mu      sync.Mutex
	scores  map[string]float64
	fail    map[string]bool
	failAll bool
	// short answers one score fewer than asked.
	short    bool
	maxBatch int
	requests []llmrelevance.Request
}

func (s *scriptedScorer) Direction() llmrelevance.Direction { return llmrelevance.Both }
func (s *scriptedScorer) MaxBatch() int                     { return s.maxBatch }
func (s *scriptedScorer) MaxInputTokens() int               { return 0 }
func (s *scriptedScorer) MaxPairTokens() int                { return 0 }

func (s *scriptedScorer) Score(_ context.Context, request llmrelevance.Request) (llmrelevance.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, request)
	result := llmrelevance.Result{Identity: pkgllm.Identity{Provider: "test", Model: "scripted"}}
	for _, candidate := range request.Candidates {
		if s.failAll || s.fail[candidate.ID] {
			return llmrelevance.Result{}, errors.New("provider unavailable")
		}
		score, ok := s.scores[candidate.ID]
		if !ok {
			score = 0.5
		}
		result.Scores = append(result.Scores, score)
	}
	if s.short {
		result.Scores = result.Scores[:len(result.Scores)-1]
	}
	return result, nil
}

func (s *scriptedScorer) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func prefilterFixture(t *testing.T, scorer *scriptedScorer) *gatherFixture {
	f := newGatherFixture(t)
	f.seed(t)
	f.prefilter = sdd.Prefilter{Scorer: scorer, Judge: gatherJudge}
	return f
}

func prefilterPolicy(from int) sdd.GroundingPolicy {
	return sdd.GroundingPolicy{BriefAllBelow: 10_000, Prefilter: true, PrefilterFrom: from}
}

func TestGather_PrefilterBelowItsThresholdRunsNoCall(t *testing.T) {
	scorer := &scriptedScorer{}
	f := prefilterFixture(t, scorer)
	application := f.app(t, prefilterPolicy(100), nil)
	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha."})
	if err != nil {
		t.Fatal(err)
	}
	if scorer.calls() != 0 {
		t.Fatalf("%d prefilter calls: below verify.prefilter_from the agent judges the briefs", scorer.calls())
	}
	if !strings.Contains(result.Text, "pairs to judge, below verify.prefilter_from (100): judge the briefs") || !strings.Contains(result.Text, "prefilter: off: ") {
		t.Fatalf("the header should say why the prefilter did not run:\n%s", result.Text)
	}
}

func TestGather_PrefilterSwitchedOffRunsNoCall(t *testing.T) {
	scorer := &scriptedScorer{}
	f := prefilterFixture(t, scorer)
	policy := prefilterPolicy(1)
	policy.Prefilter = false
	application := f.app(t, policy, nil)
	if _, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha."}); err != nil {
		t.Fatal(err)
	}
	if scorer.calls() != 0 {
		t.Fatal("a switched-off prefilter should run no call even with a scorer configured")
	}
}

func TestGather_PrefilterReadsWholeEntries(t *testing.T) {
	scorer := &scriptedScorer{}
	f := prefilterFixture(t, scorer)
	long := strings.Repeat("A constraint that matters. ", 2000) + "The final clause."
	f.write(t, "20260101-100007-s-tac-lng", "gap", long, "")
	draft := strings.Repeat("Draft sentence. ", 1000) + "Draft end."
	application := f.app(t, prefilterPolicy(1), nil)
	if _, err := gather(t, application, sdd.GatherRequest{Draft: draft, Kind: "gap"}); err != nil {
		t.Fatal(err)
	}
	if scorer.calls() != 1 {
		t.Fatalf("%d calls: an unbounded scorer should take the reading in one call", scorer.calls())
	}
	request := scorer.requests[0]
	if !strings.Contains(request.Subject, draft) || !strings.HasPrefix(request.Subject, "kind: gap") {
		t.Fatalf("the subject should carry the draft's kind and its whole text")
	}
	found := false
	for _, candidate := range request.Candidates {
		if candidate.ID == "20260101-100007-s-tac-lng" {
			found = strings.Contains(candidate.Text, long) && strings.Contains(candidate.Text, "kind: gap")
		}
	}
	if !found {
		t.Fatal("the long entry should reach the prefilter whole, with its kind")
	}
}

func TestGather_PrefilterOnlySortsUntilTheRecallBoundHolds(t *testing.T) {
	scorer := &scriptedScorer{scores: map[string]float64{gatherAlpha: 0.9, gatherBeta: 0.0, gatherUnrelated: 0.01}}
	f := prefilterFixture(t, scorer)
	application := f.app(t, prefilterPolicy(1), nil)
	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha."})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Text, model.SetAsideFiltered) || len(result.FullIDs) != 0 {
		t.Fatalf("an uncalibrated prefilter should set nothing aside and serve nothing in full:\n%s", result.Text)
	}
	if !slices.Contains(result.SummaryIDs, gatherBeta) || result.SummaryIDs[0] != gatherAlpha || result.SummaryIDs[len(result.SummaryIDs)-1] != gatherBeta {
		t.Fatalf("to judge %v should hold every candidate by falling score", result.SummaryIDs)
	}
	if !strings.Contains(result.Text, "prefilter: ok: judge "+gatherJudge+" scored ") {
		t.Fatalf("the header and the lines should show the pass and its scores:\n%s", result.Text)
	}
}

func TestGather_CalibratedPrefilterSetsAsideAndServesInFull(t *testing.T) {
	scorer := &scriptedScorer{scores: map[string]float64{gatherAlpha: 0.95, gatherBeta: 0.0, gatherUnrelated: 0.01}}
	f := prefilterFixture(t, scorer)
	f.relevance = calibratedLedger(t)
	application := f.app(t, prefilterPolicy(1), nil)
	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha."})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.FullIDs) != 1 || result.FullIDs[0] != gatherAlpha {
		t.Fatalf("full %v: a score above the relevant threshold should be served in full", result.FullIDs)
	}
	line := setAsideLine(result.Text, model.SetAsideFiltered)
	lowSetAside := (strings.Contains(line, gatherBeta) || slices.Contains(result.SummaryIDs, gatherBeta)) && (strings.Contains(line, gatherUnrelated) || slices.Contains(result.SummaryIDs, gatherUnrelated))
	if !lowSetAside || strings.Contains(line, gatherAlpha) || len(result.SummaryIDs) != 2 {
		t.Fatalf("scores below the calibrated threshold should be set aside, but for the audit lines:\n%s", result.Text)
	}
}

func TestGather_PrefilterFailureLeavesEveryAffectedCandidateToJudge(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fail   map[string]bool
		all    bool
		short  bool
		status string
	}{
		{"provider down", map[string]bool{gatherAlpha: true, gatherBeta: true, gatherUnrelated: true}, true, false, "prefilter: failed"},
		{"a short answer", map[string]bool{gatherAlpha: true, gatherBeta: true, gatherUnrelated: true}, false, true, "prefilter: failed"},
		{"one call fails", map[string]bool{gatherBeta: true}, false, false, "prefilter: partial"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scorer := &scriptedScorer{fail: tc.fail, failAll: tc.all, short: tc.short, maxBatch: 1, scores: map[string]float64{gatherAlpha: 0.95, gatherBeta: 0.0, gatherUnrelated: 0.0}}
			f := prefilterFixture(t, scorer)
			f.relevance = calibratedLedger(t)
			application := f.app(t, prefilterPolicy(1), nil)
			result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha."})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(result.Text, tc.status) || !strings.Contains(result.Text, "unanswered and left to judge (") {
				t.Fatalf("the header should name the status and the unanswered count:\n%s", result.Text)
			}
			for id := range tc.fail {
				line := lineOf(result.Text, id)
				if line == "" || strings.Contains(line, "score") || !strings.HasPrefix(line, "- ") {
					t.Fatalf("failed candidate %s should be listed to judge, unscored:\n%s", id, result.Text)
				}
			}
		})
	}
}

// calibratedLedger holds enough referenced labels by gatherJudge, all scored
// at 200, for the recall bound to hold.
func calibratedLedger(t *testing.T) *localadapter.MemoryRelevanceStore {
	t.Helper()
	var batch model.RelevanceBatch
	for i := range 189 {
		pair := model.NewRelevancePair(fmt.Sprintf("20250101-%06d-s-tac-pos", i), "20250601-000000-d-tac-sub")
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}})
		batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: gatherJudge, Score: 200}})
	}
	store := localadapter.NewMemoryRelevanceStore()
	if err := store.Append(t.Context(), relevance.Encode(batch)); err != nil {
		t.Fatal(err)
	}
	return store
}

func lineOf(text, id string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "- "+id) {
			return line
		}
	}
	return ""
}

func setAsideLine(text, reason string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "- "+reason+":") {
			return line
		}
	}
	return ""
}

func TestGather_ShowsAShareOfTheSetAsidesBlind(t *testing.T) {
	scorer := &scriptedScorer{scores: map[string]float64{gatherAlpha: 0.95}}
	f := prefilterFixture(t, scorer)
	f.relevance = calibratedLedger(t)
	application := f.app(t, prefilterPolicy(1), nil)
	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha."})
	if err != nil {
		t.Fatal(err)
	}
	setAside := setAsideLine(result.Text, model.SetAsideFiltered)
	var audited []string
	for _, id := range result.SummaryIDs {
		if line := lineOf(result.Text, id); line != "" && !strings.Contains(setAside, id) {
			audited = append(audited, id)
		}
	}
	if len(audited) != 2 {
		t.Fatalf("audited %v: ceil(3%%) of the 34 set-asides should be shown blind to judge:\n%s", audited, result.Text)
	}
	if strings.Contains(result.Text, "· score") {
		t.Fatalf("no line should show a score that tells a sample apart:\n%s", result.Text)
	}
	again, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha."})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(again.SummaryIDs, result.SummaryIDs) {
		t.Fatal("a repeated gather of one graph should audit the same entries")
	}
}

func TestGroundingStatus_ReportsTheJudgesCalibration(t *testing.T) {
	f := prefilterFixture(t, &scriptedScorer{})
	f.relevance = calibratedLedger(t)
	application := f.app(t, prefilterPolicy(1), nil)
	status, err := application.GroundingStatus(t.Context(), sdd.RequestIdentity{Subject: "tester"}, gatherProject)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"judge: " + gatherJudge, "labels: 189 referenced", "thresholds: set aside below 0.784", "audit: 3% of set-asides shown blind", "precision: 189 of 189 pairs passed in full were referenced"} {
		if !strings.Contains(status.Text, want) {
			t.Errorf("status lacks %q:\n%s", want, status.Text)
		}
	}
}

// A judge change computes its recall bound from the stored labelled pairs
// once the new judge has scored them (D36).
func TestRescoreRelevance_CalibratesANewJudgeFromStoredLabels(t *testing.T) {
	f := newGatherFixture(t)
	var batch model.RelevanceBatch
	subject := "20260201-000000-d-tac-sub"
	f.write(t, subject, "directive", "The subject decision.", "intent: pending\n")
	for i := range 189 {
		id := fmt.Sprintf("20260101-%02d%02d00-s-tac-pos", i/60, i%60)
		f.write(t, id, "gap", fmt.Sprintf("Referenced gap %d.", i), "")
		pair := model.NewRelevancePair(id, subject)
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}})
		batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{Judge: "old:judge", Score: 220}})
	}
	store := localadapter.NewMemoryRelevanceStore()
	if err := store.Append(t.Context(), relevance.Encode(batch)); err != nil {
		t.Fatal(err)
	}
	f.relevance = store
	scorer := &scriptedScorer{scores: map[string]float64{}}
	f.prefilter = sdd.Prefilter{Scorer: scorer, Judge: "new:judge"}
	application := f.app(t, prefilterPolicy(1), nil)
	identity := sdd.RequestIdentity{Subject: "tester"}

	before, err := application.GroundingStatus(t.Context(), identity, gatherProject)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before.Text, "unscored: 189 labelled pairs") || !strings.Contains(before.Text, "thresholds: none yet") {
		t.Fatalf("a new judge should start without thresholds and name the unscored pairs:\n%s", before.Text)
	}
	result, err := application.RescoreRelevance(t.Context(), identity, gatherProject)
	if err != nil {
		t.Fatal(err)
	}
	if result.Scored != 189 || scorer.calls() != 1 {
		t.Fatalf("result %+v after %d calls: one pass should score every labelled pair", result, scorer.calls())
	}
	if !strings.Contains(scorer.requests[0].Subject, "The subject decision.") {
		t.Fatal("each pair should be scored from its later entry, the one that judged it")
	}
	after, err := application.GroundingStatus(t.Context(), identity, gatherProject)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after.Text, "thresholds: set aside below") || strings.Contains(after.Text, "unscored:") {
		t.Fatalf("after one pass the new judge should hold its bound:\n%s", after.Text)
	}
}

// An entry the prefilter serves in full that the consumer already holds prints
// as its pointer, and still counts as read; opened by ID it is the way back
// and serves in full (20261109-004506-d-cpt-udz, R14, D34).
func TestGather_RelevantEntryTheConsumerHoldsPrintsAsItsPointer(t *testing.T) {
	scorer := &scriptedScorer{scores: map[string]float64{gatherAlpha: 0.95, gatherBeta: 0.0, gatherUnrelated: 0.01}}
	f := prefilterFixture(t, scorer)
	f.relevance = calibratedLedger(t)
	application := f.app(t, prefilterPolicy(1), nil)
	held := func(id string) (string, bool) { return "ab12", id == gatherAlpha }
	pointer := gatherAlpha + " — The alpha entry is about alpha matters. (held as ⟦ab12⟧)"

	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha.", Held: held})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, pointer) || strings.Contains(result.Text, "id: "+gatherAlpha) {
		t.Fatalf("a relevant entry the consumer holds should print as its pointer:\n%s", result.Text)
	}
	if !strings.Contains(result.Text, presenters.GatherWayBack) {
		t.Fatalf("the pointer should name the way back:\n%s", result.Text)
	}
	if !slices.Contains(result.FullIDs, gatherAlpha) {
		t.Fatalf("the pointed entry should still count as read in full: %v", result.FullIDs)
	}

	opened, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha.", IDs: []string{gatherAlpha}, Held: held})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(opened.Text, pointer) || strings.Contains(opened.Text, "id: "+gatherAlpha) {
		t.Fatalf("an entry opened by ID that the consumer holds should print as its pointer:\n%s", opened.Text)
	}
	if !slices.Contains(opened.FullIDs, gatherAlpha) {
		t.Fatalf("the opened entry should count as read in full: %v", opened.FullIDs)
	}

	unproved := func(string) (string, bool) { return "", false }
	reopened, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha.", IDs: []string{gatherAlpha}, Held: unproved})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reopened.Text, "id: "+gatherAlpha) {
		t.Fatalf("an entry opened by ID without its proof should serve in full:\n%s", reopened.Text)
	}
}
