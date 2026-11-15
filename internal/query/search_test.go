package query_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

func TestSearchQuery_TextFallback(t *testing.T) {
	tests := []struct {
		name  string
		query query.SearchQuery
		terms []string
	}{
		{name: "phrase words become one alternation", query: query.SearchQuery{Phrase: "Deploy stamp, deploy pods!"}, terms: []string{"(deploy|stamp|pods)"}},
		{name: "short words are dropped", query: query.SearchQuery{Phrase: "a pin of go"}, terms: []string{"(pin)"}},
		{name: "terms are kept beside the phrase", query: query.SearchQuery{Terms: []string{"lab"}, Phrase: "restart pods"}, terms: []string{"lab", "(restart|pods)"}},
		{name: "a phrase without words is matched literally", query: query.SearchQuery{Phrase: " a.b "}, terms: []string{`a\.b`}},
		{name: "text query stays as it is", query: query.SearchQuery{Terms: []string{"lab"}}, terms: []string{"lab"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.query.TextFallback()
			if got.Mode() != query.SearchModeText {
				t.Errorf("fallback should be a text query, got mode %q", got.Mode())
			}
			if !slices.Equal(got.Terms, tt.terms) {
				t.Errorf("fallback terms should carry the phrase words, got %q", got.Terms)
			}
		})
	}
}

func TestSimilarTitles_NeedsABodyToSearchBy(t *testing.T) {
	_, ok := query.SimilarTitles(&model.Entry{ID: "20260101-100000-s-tac-aaa", Content: " \n"})
	if ok {
		t.Error("an entry without a body should have nothing to search by")
	}
	q, ok := query.SimilarTitles(&model.Entry{ID: "20260101-100000-s-tac-aaa", Content: "A body."})
	if !ok || q.Phrase != "A body." || q.Limit != model.SimilarTitleCount || q.SyncMode != query.SearchSyncNone {
		t.Errorf("the query should search the body for the similar-title count without syncing, got %+v", q)
	}
	if q.Admits(&model.Entry{ID: "20260101-100000-s-tac-aaa", Title: "Own"}) || q.Admits(&model.Entry{ID: "20260101-100000-s-tac-bbb"}) {
		t.Error("the query should admit neither the entry itself nor an untitled entry")
	}
	if !q.Admits(&model.Entry{ID: "20260101-100000-s-tac-bbb", Title: "Other"}) {
		t.Error("the query should admit another titled entry")
	}
}

func TestSimilarTitles_LeavesTheLinkedEntriesOut(t *testing.T) {
	entry := &model.Entry{
		ID: "20260101-100000-d-tac-aaa", Content: "A body.",
		Refs:       []model.Ref{{ID: "20260101-100000-s-tac-ref"}},
		Closes:     []string{"20260101-100000-s-tac-cls"},
		Supersedes: []string{"20260101-100000-d-tac-sup"},
	}
	q, ok := query.SimilarTitles(entry)
	if !ok {
		t.Fatal("an entry with a body should have a query")
	}
	for _, id := range []string{"20260101-100000-s-tac-ref", "20260101-100000-s-tac-cls", "20260101-100000-d-tac-sup"} {
		if q.Admits(&model.Entry{ID: id, Title: "Linked"}) {
			t.Errorf("the query should leave out %s, which the prompt lists as a linked entry", id)
		}
	}
}
