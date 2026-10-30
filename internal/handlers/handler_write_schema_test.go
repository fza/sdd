package handlers_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
)

func TestWritesRefuseAGraphSchemaUnknownToTheBinary(t *testing.T) {
	const entryID = "20260427-120000-s-prc-tx1"
	summary := "A summary."

	tests := []struct {
		name  string
		write func(context.Context, *handlers.Handler) error
	}{
		{
			name: "new entry",
			write: func(ctx context.Context, h *handlers.Handler) error {
				return h.NewEntry(ctx, &command.NewEntryCmd{
					Type: model.TypeSignal, Layer: model.LayerTactical, Description: "Body.",
					Summary: summary, SkipPreflight: true,
				})
			},
		},
		{
			name: "summarize",
			write: func(ctx context.Context, h *handlers.Handler) error {
				return h.Summarize(ctx, &command.SummarizeCmd{EntryIDs: []string{entryID}, ExplicitText: &summary})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graphDir := t.TempDir()
			sddDir := filepath.Join(graphDir, ".sdd")
			err := os.MkdirAll(sddDir, 0o755)
			if err != nil {
				t.Fatal(err)
			}
			writeEntryFile(t, graphDir, entryID, "---\ntype: signal\nlayer: process\nkind: gap\n---\n\nBody.\n")
			committer := &recordingCommitter{}
			h := handlers.New(handlers.Options{
				GraphDir:  graphDir,
				SDDDir:    sddDir,
				Reader:    &fakeReader{graphSchema: model.CurrentGraphSchemaVersion + 1},
				Committer: committer,
			})

			err = tt.write(t.Context(), h)
			if !errors.Is(err, model.ErrUnknownSchemaVersion) {
				t.Fatalf("a write to a graph at an unknown schema should be refused as unknown to the binary, got %v", err)
			}
			if committer.calls != 0 {
				t.Error("a refused write should commit nothing")
			}
		})
	}
}
