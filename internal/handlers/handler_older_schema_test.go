package handlers_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

func TestNewEntryOnASchemaOneGraphWritesNoTitle(t *testing.T) {
	cases := []struct {
		name        string
		summary     string
		wantCalls   map[llm.Purpose]int
		wantSummary string
	}{
		{name: "a supplied summary costs no call", summary: "A supplied summary.", wantCalls: map[llm.Purpose]int{}, wantSummary: "A supplied summary."},
		{name: "a generated summary drops its title", wantCalls: map[llm.Purpose]int{llm.PurposeSummarize: 1}, wantSummary: "GENERATED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			graphDir := t.TempDir()
			runner := &stubRunner{text: "GENERATED"}
			h := handlers.New(handlers.Options{
				GraphDir: graphDir, SDDDir: filepath.Join(graphDir, ".sdd"), Reader: &fakeReader{graphSchema: 1},
				Committer: &recordingCommitter{}, LLMRunner: runner, Stderr: &bytes.Buffer{},
			})
			var id string
			err := h.NewEntry(context.Background(), &command.NewEntryCmd{
				Type: model.TypeSignal, Layer: model.LayerTactical, Description: "a gap on an older graph",
				Summary: tc.summary, SkipPreflight: true,
				OnNewEntry: func(newID, _ string) { id = newID },
			})
			if err != nil {
				t.Fatalf("NewEntry: %v", err)
			}
			if runner.purposeCount(llm.PurposeTitle) != 0 || runner.purposeCount(llm.PurposeSummarize) != tc.wantCalls[llm.PurposeSummarize] {
				t.Errorf("a schema-1 write should make no title call, got %v", runner.byUse)
			}
			content := readEntryFile(t, graphDir, id)
			if strings.Contains(content, "\ntitle:") {
				t.Errorf("a schema-1 entry should carry no title:\n%s", content)
			}
			if !strings.Contains(content, "summary: "+tc.wantSummary) {
				t.Errorf("the summary should still be written:\n%s", content)
			}
		})
	}
}

func TestNewEntryOnASchemaOneGraphRefusesAnExplicitTitle(t *testing.T) {
	graphDir := t.TempDir()
	runner := &stubRunner{text: "GENERATED"}
	h := handlers.New(handlers.Options{
		GraphDir: graphDir, SDDDir: filepath.Join(graphDir, ".sdd"), Reader: &fakeReader{graphSchema: 1},
		Committer: &recordingCommitter{}, LLMRunner: runner, Stderr: &bytes.Buffer{},
	})
	err := h.NewEntry(context.Background(), &command.NewEntryCmd{
		Type: model.TypeSignal, Layer: model.LayerTactical, Description: "a gap on an older graph",
		Summary: "S.", Title: "A title", SkipPreflight: true,
	})
	if err == nil || !strings.Contains(err.Error(), "schema 1") {
		t.Fatalf("an explicit title on a schema-1 graph should be refused naming the schema, got %v", err)
	}
	if runner.callCount() != 0 {
		t.Errorf("a refused write should make no model call, got %v", runner.byUse)
	}
	written, readErr := os.ReadDir(graphDir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(written) != 0 {
		t.Errorf("a refused write should leave the graph untouched, found %v", written)
	}
}

func TestSummarizeAllOnASchemaOneGraphFillsOnlySummaries(t *testing.T) {
	graphDir := t.TempDir()
	writeSummarizePair(t, graphDir)
	runner := &stubRunner{text: "GENERATED"}
	h := handlers.New(handlers.Options{
		GraphDir: graphDir, SDDDir: filepath.Join(graphDir, ".sdd"), Reader: &graphReader{graphSchema: 1},
		Committer: &recordingCommitter{}, LLMRunner: runner,
	})
	var skipped []string
	err := h.Summarize(context.Background(), &command.SummarizeCmd{OnSkipped: func(id string) { skipped = append(skipped, id) }})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if runner.purposeCount(llm.PurposeTitle) != 0 || runner.purposeCount(llm.PurposeSummarize) != 1 {
		t.Errorf("only the entry without a summary should cost a call, and no title call, got %v", runner.byUse)
	}
	if len(skipped) != 1 || skipped[0] != summarizedEntry {
		t.Errorf("an entry with a summary is complete at schema 1, got skipped %v", skipped)
	}
	if empty := readEntryFile(t, graphDir, emptyEntry); strings.Contains(empty, "\ntitle:") || !strings.Contains(empty, "summary: GENERATED") {
		t.Errorf("the filled entry should gain a summary and no title:\n%s", empty)
	}
}

func TestSummarizeOnASchemaOneGraphRefusesAnExplicitTitle(t *testing.T) {
	graphDir := t.TempDir()
	writeSummarizePair(t, graphDir)
	h := handlers.New(handlers.Options{
		GraphDir: graphDir, SDDDir: filepath.Join(graphDir, ".sdd"), Reader: &graphReader{graphSchema: 1},
		Committer: &recordingCommitter{},
	})
	title := "A title"
	err := h.Summarize(context.Background(), &command.SummarizeCmd{EntryIDs: []string{summarizedEntry}, ExplicitTitle: &title})
	if err == nil || !strings.Contains(err.Error(), "schema 1") {
		t.Fatalf("an explicit title on a schema-1 graph should be refused naming the schema, got %v", err)
	}
	if kept := readEntryFile(t, graphDir, summarizedEntry); strings.Contains(kept, "\ntitle:") {
		t.Errorf("a refused title should leave the entry untouched:\n%s", kept)
	}
}
