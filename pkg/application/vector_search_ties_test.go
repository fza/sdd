package application_test

import (
	"fmt"
	"slices"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
)

func tiedID(i int) string { return fmt.Sprintf("20260103-1000%02d-s-tac-t%02d", i, i) }

// Equal scores are ordered by entry ID before the limit cuts them, so a
// repeated search returns the same hits in the same order.
func TestSearch_EqualScoresRankByID(t *testing.T) {
	graphDir, cacheRoot := t.TempDir(), t.TempDir()
	for i := range 30 {
		body := "## Section\nA tied entry."
		if i >= 20 {
			body = "## Section\nA tied zeta entry."
		}
		writeCounterEntry(t, graphDir, tiedID(i), fmt.Sprintf("Tied entry %d", i), body)
	}
	application := newCounterApp(t, graphDir, cacheRoot, &countingEmbeddings{})
	for _, tc := range []struct {
		name  string
		terms []string
		want  []string
	}{
		{name: "vector", want: []string{tiedID(0), tiedID(1), tiedID(2), tiedID(3), tiedID(4)}},
		// Vector rank n and text rank n fuse to the same score.
		{name: "hybrid", terms: []string{"zeta"}, want: []string{tiedID(0), tiedID(20), tiedID(1), tiedID(21), tiedID(2)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for range 5 {
				result, err := application.Search(t.Context(), sdd.RequestIdentity{Subject: "christopher"}, counterProject, sdd.SearchRequest{
					SyncMode: sdd.SearchSyncAll, Phrase: "tied", Terms: tc.terms, Limit: 5, MaxCitations: 1,
				})
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(result.EntryIDs, tc.want) {
					t.Fatalf("hits %v: equal scores should rank by ID", result.EntryIDs)
				}
			}
		})
	}
}
