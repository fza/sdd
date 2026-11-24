package relevance_test

import (
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// lineScorer is a fixedScorer that reads a candidate's Line alone, as a
// chat scorer does.
type lineScorer struct{ *fixedScorer }

func (lineScorer) Reads(candidate relevance.Candidate) bool { return candidate.Line != "" }

// A candidate the scorer does not read goes into no call and is reported
// apart from the oversize ones; Readable keeps the others.
func TestPlan_LeavesOutWhatTheScorerDoesNotRead(t *testing.T) {
	scorer := lineScorer{&fixedScorer{maxBatch: 2, maxTokens: 50}}
	planned := []relevance.Candidate{
		{ID: "a", Text: "a", Line: "a"},
		{ID: "b", Text: "b"},
		{ID: "c", Text: strings.Repeat("x", 300), Line: "c"},
		{ID: "d", Text: "d", Line: "d"},
		{ID: "e", Text: "e", Line: "e"},
	}
	batches, oversize, unread := relevance.Plan(scorer, "subject", planned)
	if !reflect.DeepEqual(batches, [][]int{{0, 3}, {4}}) || !slices.Equal(oversize, []int{2}) || !slices.Equal(unread, []int{1}) {
		t.Fatalf("Plan = %v, oversize %v, unread %v: no call should carry the candidate the scorer does not read", batches, oversize, unread)
	}
	var ids []string
	for _, candidate := range relevance.Readable(scorer, planned) {
		ids = append(ids, candidate.ID)
	}
	if !slices.Equal(ids, []string{"a", "c", "d", "e"}) {
		t.Fatalf("Readable = %v: it should keep every candidate the scorer reads, in order", ids)
	}
}

// A scorer without Reads reads every candidate; the decorators hand on the
// Reads of a scorer that has one.
func TestReads_IsOptionalAndPassesThroughTheDecorators(t *testing.T) {
	planned := []relevance.Candidate{{ID: "a", Text: "a", Line: "a"}, {ID: "b", Text: "b"}}
	if batches, _, unread := relevance.Plan(&fixedScorer{}, "subject", planned); !reflect.DeepEqual(batches, [][]int{{0, 1}}) || unread != nil {
		t.Fatalf("Plan = %v, unread %v: a scorer without Reads should read every candidate", batches, unread)
	}
	lines := lineScorer{&fixedScorer{}}
	for name, scorer := range map[string]relevance.Scorer{
		"bounded":      relevance.Bounded(lines, time.Second),
		"rate limited": relevance.RateLimited(lines, 10),
		"limited":      relevance.Limited(lines, 1),
		"observed":     relevance.Observed(lines, &statSink{}),
	} {
		if _, _, unread := relevance.Plan(scorer, "subject", planned); !slices.Equal(unread, []int{1}) {
			t.Errorf("%s: unread %v: the decorator should hand on the inner scorer's Reads", name, unread)
		}
	}
}

func TestLimited_HoldsTheCallsInFlight(t *testing.T) {
	inner := &fixedScorer{maxBatch: 100, entered: make(chan struct{}, 8), release: make(chan struct{})}
	scorer := relevance.Limited(inner, 2)
	if scorer.MaxBatch() != 100 {
		t.Fatal("Limited should keep the inner scorer's limits")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, _ = scorer.Score(t.Context(), relevance.Request{Subject: "s", Candidates: candidates("a")})
		})
	}
	<-inner.entered
	<-inner.entered
	select {
	case <-inner.entered:
		t.Error("a third call should wait for a slot")
	case <-time.After(50 * time.Millisecond):
	}
	close(inner.release)
	wg.Wait()
}
