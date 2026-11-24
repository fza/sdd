package relevance_test

import (
	"reflect"
	"slices"
	"strings"
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
		"observed":     relevance.Observed(lines, &statSink{}),
		"per item":     relevance.PerItem(lines, 1),
	} {
		if _, _, unread := relevance.Plan(scorer, "subject", planned); !slices.Equal(unread, []int{1}) {
			t.Errorf("%s: unread %v: the decorator should hand on the inner scorer's Reads", name, unread)
		}
	}
}
