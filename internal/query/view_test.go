package query_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/query"
)

func TestViewResult_BodiesOnly(t *testing.T) {
	cases := []struct {
		name    string
		renders []string
		want    bool
	}{
		{name: "every section serves bodies", renders: []string{"as-bodies", "as-bodies"}, want: true},
		{name: "a list beside the bodies", renders: []string{"as-bodies", "as-list"}, want: false},
		{name: "no section", renders: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var result query.ViewResult
			for _, render := range tc.renders {
				result.Sections = append(result.Sections, query.SectionResult{Render: render})
			}
			if got := result.BodiesOnly(); got != tc.want {
				t.Errorf("BodiesOnly() = %v for sections %v", got, tc.renders)
			}
		})
	}
}
