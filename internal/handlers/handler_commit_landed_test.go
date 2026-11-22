package handlers_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
)

// landedCommitter commits, then fails to set up the entry merge driver.
type landedCommitter struct{ commits int }

func (c *landedCommitter) Commit(string, ...string) error {
	c.commits++
	return nil
}

func (c *landedCommitter) EnsureMergeDriver(context.Context) error {
	return errors.New("config locked")
}

// A failure after the commit landed names the step that failed and how to
// finish it; it never claims the written files stayed uncommitted.
func TestCommitThatLandedIsNotReportedUncommitted(t *testing.T) {
	text, title := "A summary set by hand.", "Hand-set title"
	for _, tc := range []struct {
		name string
		run  func(t *testing.T, h *handlers.Handler) error
	}{
		{name: "summarize --all", run: func(_ *testing.T, h *handlers.Handler) error {
			return h.Summarize(context.Background(), &command.SummarizeCmd{})
		}},
		{name: "summarize with --text", run: func(_ *testing.T, h *handlers.Handler) error {
			return h.Summarize(context.Background(), &command.SummarizeCmd{EntryIDs: []string{summarizedEntry}, ExplicitText: &text})
		}},
		{name: "new", run: func(_ *testing.T, h *handlers.Handler) error {
			return h.NewEntry(context.Background(), &command.NewEntryCmd{
				Type: model.TypeSignal, Layer: model.LayerTactical, Description: "A capture whose commit lands.",
				Summary: "The capture commit lands.", Title: &title, SkipPreflight: true,
			})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			graphDir := t.TempDir()
			sddDir := filepath.Join(graphDir, ".sdd")
			if err := os.MkdirAll(sddDir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeSummarizePair(t, graphDir)
			committer := &landedCommitter{}
			h := handlers.New(handlers.Options{
				GraphDir: graphDir, SDDDir: sddDir, Reader: &graphReader{}, Committer: committer, Stderr: &bytes.Buffer{},
				LLMRunner: &stubRunner{text: "GENERATED", numbered: true},
			})

			err := tc.run(t, h)

			if committer.commits != 1 {
				t.Fatalf("the commit should land once, got %d commits", committer.commits)
			}
			if err == nil || !strings.Contains(err.Error(), "landed, setting up the entry merge driver failed (sdd init sets it up)") {
				t.Errorf("the error should name the failed setup after the landed commit and how to finish it, got %v", err)
			}
			if strings.Contains(err.Error(), "uncommitted") || strings.HasPrefix(err.Error(), "git commit:") || errors.Is(err, model.ErrIncompleteWrite) {
				t.Errorf("a landed commit should not be reported as a failed commit or uncommitted files, got %v", err)
			}
		})
	}
}
