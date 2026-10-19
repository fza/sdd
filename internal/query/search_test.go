package query_test

import (
	"slices"
	"testing"

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
