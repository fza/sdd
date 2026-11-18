package llmops_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
)

// The ranking note names what ranked the reading, with counts.
func TestDeepRankingNote(t *testing.T) {
	for name, tc := range map[string]struct {
		sources model.RankingSources
		widened bool
		want    string
	}{
		"judge":      {model.RankingSources{Judged: 40}, false, "Ranking source: judge chat:claude-haiku-5-5, 40 entries.\n"},
		"similarity": {model.RankingSources{Similar: 12}, false, "Ranking source: similarity to the draft, 12 entries: no judge scored them.\n"},
		"mixed":      {model.RankingSources{Judged: 40, Similar: 12, Listed: 3}, false, "Ranking source: mixed, 40 by judge chat:claude-haiku-5-5, 12 by similarity to the draft, 3 in listed order.\n"},
		"widened":    {model.RankingSources{Judged: 2}, true, "so the reading serves the top 10 in full and lists the next 200 with their summary.\n"},
	} {
		t.Run(name, func(t *testing.T) {
			got := llmops.DeepRankingNote(tc.sources, "chat:claude-haiku-5-5", tc.widened)
			if !strings.HasPrefix(got, "\n## Ranking\n\n") || !strings.HasSuffix(got, tc.want) {
				t.Fatalf("note %q should end %q", got, tc.want)
			}
		})
	}
	if got := llmops.DeepRankingNote(model.RankingSources{}, "", false); got != "" {
		t.Fatalf("note %q: nothing ranked heads nothing", got)
	}
}
