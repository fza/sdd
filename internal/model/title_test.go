package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestTitleSurvivesTheFrontmatterRoundTrip(t *testing.T) {
	entry := &model.Entry{ID: "20260101-100000-s-tac-ttl", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical, Title: "daemon project removal", Summary: "S.", Content: "Body."}
	written := model.FormatFrontmatter(entry) + "\n" + entry.Content + "\n"
	if !strings.Contains(written, "title: daemon project removal") {
		t.Fatalf("the title should be written to the frontmatter:\n%s", written)
	}
	parsed, err := model.ParseEntry(entry.ID+".md", written)
	if err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
	if parsed.Title != entry.Title {
		t.Errorf("the title should read back unchanged, got %q", parsed.Title)
	}
}

func TestListingTitle(t *testing.T) {
	cases := []struct {
		name  string
		entry model.Entry
		want  string
	}{
		{name: "the title names a titled entry", entry: model.Entry{Title: "daemon project removal", Summary: "The daemon project goes. It was unused."}, want: "daemon project removal"},
		{name: "an untitled entry falls back to its first summary sentence", entry: model.Entry{Summary: "The daemon project goes. It was unused."}, want: "The daemon project goes."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.entry.ListingTitle(); got != tc.want {
				t.Errorf("listing title should name the entry, got %q", got)
			}
		})
	}
}

func TestTitleBackfill(t *testing.T) {
	entries := []*model.Entry{
		{Summary: "S."},
		{Summary: "S.", Title: "has both"},
		{},
		{Embedded: true, Summary: "S."},
	}
	withSummary, titleOnly := model.TitleBackfill(entries)
	if withSummary != 1 || titleOnly != 1 {
		t.Errorf("only the entry lacking both owes a paired call and only the summarized one a title call, got %d paired, %d title-only", withSummary, titleOnly)
	}
}

func TestCheckCompatibilityPointsAnOlderGraphAtMigrate(t *testing.T) {
	cases := []struct {
		name       string
		graph      int
		wantReason string
	}{
		{name: "older graph", graph: model.CurrentGraphSchemaVersion - 1, wantReason: "sdd migrate"},
		{name: "newer graph", graph: model.CurrentGraphSchemaVersion + 1, wantReason: "upgrade sdd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := model.CheckCompatibility(model.SchemaMeta{GraphSchemaVersion: tc.graph}, "1.0.0", model.CurrentGraphSchemaVersion)
			if res.Compatible {
				t.Fatal("a schema mismatch should refuse writes")
			}
			if !strings.Contains(res.Reason, tc.wantReason) {
				t.Errorf("the refusal should name the way forward, got %q", res.Reason)
			}
		})
	}
}
