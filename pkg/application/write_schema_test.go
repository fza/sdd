package application_test

import (
	"errors"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

func TestWritesRefuseAGraphSchemaUnknownToTheBinary(t *testing.T) {
	tests := []struct {
		name  string
		write func(f *writeFixture) error
	}{
		{name: "capture", write: func(f *writeFixture) error {
			draft := f.identifiedDraft(sdd.EntryDraft{Kind: "gap", Layer: "tactical", Confidence: "high", Body: "A gap."})
			_, err := f.app.CreateEntry(t.Context(), f.identity, "example", f.binding, draft)
			return err
		}},
		{name: "title correction", write: func(f *writeFixture) error {
			_, err := f.app.ReplaceSummary(t.Context(), f.identity, "example", f.binding, sdd.SummaryReplacement{
				Publication: sdd.PublicationKey{Session: f.binding.SessionID, Sequence: 1, Discriminator: "summary"},
				EntryID:     "20260427-120000-s-prc-tx1",
				Summary:     "A summary.",
				Title:       "A title",
			})
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newWriteFixture(t, writeFixtureOptions{GraphSchema: model.CurrentGraphSchemaVersion + 1})
			err := tt.write(f)
			if !errors.Is(err, model.ErrUnknownSchemaVersion) {
				t.Fatalf("a write to a graph at an unknown schema should be refused as unknown to the binary, got %v", err)
			}
		})
	}
}
