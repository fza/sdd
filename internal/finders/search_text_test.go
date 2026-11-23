package finders_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

func textEntry(id, summary string) *model.Entry {
	return &model.Entry{ID: id, Type: model.TypeSignal, Layer: model.LayerTactical, Kind: model.KindGap, Summary: summary, Content: summary}
}

func textSearchIDs(t *testing.T, entries []*model.Entry, q query.SearchQuery) []string {
	t.Helper()
	f := finders.NewSearchFinder(finders.SearchFinderOptions{Graph: model.NewGraph(entries), GraphDir: t.TempDir()})
	res, err := f.Search(t.Context(), q)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var ids []string
	for _, e := range res.Entries {
		ids = append(ids, e.Entry.ID)
	}
	slices.Sort(ids)
	return ids
}

func TestSearchFinder_TextModeWholeWords(t *testing.T) {
	t.Parallel()
	entries := []*model.Entry{
		textEntry("20260101-100000-s-tac-ttl", "Prices are kept with a TTL of one hour."),
		textEntry("20260101-100001-s-tac-set", "Settle the little dispute first."),
		textEntry("20260101-100002-s-tac-edg", "(TTL) and ttl."),
		textEntry("20260101-100003-s-tac-und", "The price_ttl field and the TTLs."),
		textEntry("20260101-100004-s-tac-uml", "Die Größe zählt."),
		textEntry("20260101-100005-s-tac-gro", "Die Größenordnung zählt."),
		textEntry("20260101-100006-s-tac-plu", "Written in C++ mostly."),
		textEntry("20260101-100007-s-tac-ple", "Written in C++11 mostly."),
	}
	cases := []struct {
		name       string
		term       string
		wholeWords bool
		want       []string
	}{
		{name: "a term inside a longer word does not match", term: "TTL", wholeWords: true, want: []string{"20260101-100000-s-tac-ttl", "20260101-100002-s-tac-edg"}},
		{name: "without whole words a term matches inside words", term: "TTL", want: []string{"20260101-100000-s-tac-ttl", "20260101-100001-s-tac-set", "20260101-100002-s-tac-edg", "20260101-100003-s-tac-und"}},
		{name: "letters beyond ASCII continue a word", term: "Größe", wholeWords: true, want: []string{"20260101-100004-s-tac-uml"}},
		{name: "an edge that is no word character needs no boundary", term: `C\+\+`, wholeWords: true, want: []string{"20260101-100006-s-tac-plu", "20260101-100007-s-tac-ple"}},
		{name: "a phrase matches as whole words", term: "the little", wholeWords: true, want: []string{"20260101-100001-s-tac-set"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ids := textSearchIDs(t, entries, query.SearchQuery{Terms: []string{tc.term}, WholeWords: tc.wholeWords})
			if !slices.Equal(ids, tc.want) {
				t.Errorf("term %q found the wrong entries: %v", tc.term, ids)
			}
		})
	}
}
