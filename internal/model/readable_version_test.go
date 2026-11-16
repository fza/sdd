package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestReadableVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		candidates []string
		published  map[string]bool
		want       string
	}{
		{name: "no candidates read nothing"},
		{name: "a single cap reads its hash unchecked", candidates: []string{"new"}, want: "new"},
		{name: "published new-cap version wins", candidates: []string{"new", "old"}, published: map[string]bool{"new": true, "old": true}, want: "new"},
		{name: "unpublished new-cap version falls back to the old cap", candidates: []string{"new", "old"}, published: map[string]bool{"old": true}, want: "old"},
		{name: "nothing published falls back to the oldest cap", candidates: []string{"new", "old"}, want: "old"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := model.ReadableVersion(tc.candidates, tc.published); got != tc.want {
				t.Errorf("search should read the newest published version, else the oldest cap's, got %q", got)
			}
		})
	}
}
