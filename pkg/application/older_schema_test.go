package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/llm"
)

func TestCreateEntryOnASchemaOneGraphWritesNoTitle(t *testing.T) {
	f := newWriteFixture(t, writeFixtureOptions{GraphSchema: 1, Runner: llm.RunnerFunc(func(_ context.Context, request llm.Request) (llm.Result, error) {
		if request.Purpose == llm.PurposeTitle {
			t.Error("a schema-1 capture should make no title call")
		}
		return llm.Result{Text: `{"title": "test title", "summary": "A captured entry for the project record."}`}, nil
	})})
	draft := f.identifiedDraft(sdd.EntryDraft{Kind: "gap", Layer: "tactical", Confidence: "high", Body: "An older graph keeps its schema."})
	created, err := f.app.CreateEntry(t.Context(), f.identity, "example", f.binding, draft)
	if err != nil {
		t.Fatal(err)
	}
	entry := loadEntryByID(t, f.graphDir, created.EntryID)
	if entry.Title != "" {
		t.Errorf("a schema-1 capture should carry no title, got %q", entry.Title)
	}
	if entry.Summary == "" {
		t.Error("a schema-1 capture should still carry its summary")
	}
}

func TestReplaceSummaryOnASchemaOneGraphRefusesATitle(t *testing.T) {
	f := newWriteFixture(t, writeFixtureOptions{GraphSchema: 1})
	draft := f.identifiedDraft(sdd.EntryDraft{Kind: "gap", Layer: "tactical", Confidence: "high", Body: "An older graph keeps its schema."})
	created, err := f.app.CreateEntry(t.Context(), f.identity, "example", f.binding, draft)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.app.ReplaceSummary(t.Context(), f.identity, "example", f.binding, sdd.SummaryReplacement{
		Publication: sdd.PublicationKey{Session: f.binding.SessionID, Sequence: 99, Discriminator: "replace-summary"},
		EntryID:     created.EntryID, Title: "A corrected title",
	})
	if err == nil || !strings.Contains(err.Error(), "schema 1") {
		t.Fatalf("a title correction on a schema-1 graph should be refused naming the schema, got %v", err)
	}
	if entry := loadEntryByID(t, f.graphDir, created.EntryID); entry.Title != "" {
		t.Errorf("a refused correction should leave the entry untitled, got %q", entry.Title)
	}
}

func TestInfoReportsTheGraphSchema(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema int
		want   int
	}{
		{name: "older graph", schema: 1, want: 1},
		{name: "graph without meta.json", want: model.CurrentGraphSchemaVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWriteFixture(t, writeFixtureOptions{GraphSchema: tc.schema})
			info, err := f.app.Info(t.Context(), f.identity, "example", sdd.InfoRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if info.GraphSchema != tc.want || info.BinarySchema != model.CurrentGraphSchemaVersion {
				t.Errorf("info should report the graph's and the binary's schema, got graph %d binary %d", info.GraphSchema, info.BinarySchema)
			}
		})
	}
}
