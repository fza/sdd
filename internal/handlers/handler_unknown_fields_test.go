package handlers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
)

// unknownFields is frontmatter another sdd build writes and this one does not read.
const unknownFields = "title: Fields another build writes\n" +
	"reason: |-\n" +
	"    Kept across rewrites.\n" +
	"approves:\n" +
	"    - 20260101-100000-s-tac-aaa\n"

func assertKeepsUnknownFields(t *testing.T, graphDir, id string) {
	t.Helper()
	if text := readEntryFile(t, graphDir, id); !strings.Contains(text, unknownFields) {
		t.Errorf("%s lost unknown frontmatter fields:\n%s", id, text)
	}
}

func TestSummarizeKeepsUnknownFields(t *testing.T) {
	graphDir := t.TempDir()
	id := "20260427-120000-s-prc-tx1"
	writeEntryFile(t, graphDir, id, "---\ntype: signal\nlayer: process\nkind: gap\n"+unknownFields+"---\n\nA gap.\n")

	h := handlers.New(handlers.Options{GraphDir: graphDir, Reader: &graphReader{}, Committer: &recordingCommitter{}})
	summary := "A new summary."
	if err := h.Summarize(context.Background(), &command.SummarizeCmd{EntryIDs: []string{id}, ExplicitText: &summary}); err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	if text := readEntryFile(t, graphDir, id); !strings.Contains(text, "summary: A new summary.") {
		t.Errorf("summary not written:\n%s", text)
	}
	assertKeepsUnknownFields(t, graphDir, id)
}

func TestRewriteEntryKeepsUnknownFields(t *testing.T) {
	graphDir := t.TempDir()
	target := "20260418-120000-s-tac-abc"
	inbound := "20260418-130000-s-tac-def"
	writeEntryFile(t, graphDir, target, "---\ntype: signal\nlayer: tactical\nkind: gap\n"+unknownFields+"---\n\nA gap that is a decision.\n")
	writeEntryFile(t, graphDir, inbound, "---\ntype: signal\nlayer: tactical\nkind: gap\nrefs:\n    - "+target+"\n"+unknownFields+"---\n\nRefers to the gap.\n")

	h := handlers.New(handlers.Options{GraphDir: graphDir, Reader: &graphReader{}, Committer: &recordingCommitter{}, Mover: &recordingMover{}})
	if err := h.RewriteEntry(context.Background(), &command.RewriteEntryCmd{EntryID: target, NewType: model.TypeDecision, NewKind: model.KindDirective}); err != nil {
		t.Fatalf("RewriteEntry: %v", err)
	}

	assertKeepsUnknownFields(t, graphDir, "20260418-120000-d-tac-abc")
	if text := readEntryFile(t, graphDir, inbound); !strings.Contains(text, "20260418-120000-d-tac-abc") {
		t.Errorf("inbound ref not rewritten:\n%s", text)
	}
	assertKeepsUnknownFields(t, graphDir, inbound)
}

func TestLintFixKeepsUnknownFields(t *testing.T) {
	graphDir := t.TempDir()
	id := "20260414-120000-d-tac-abc"
	writeEntryFile(t, graphDir, id, "---\ntype: decision\nlayer: tactical\n"+unknownFields+"---\n\nA decision without kind.\n")

	h := handlers.New(handlers.Options{
		GraphDir:  graphDir,
		Reader:    finders.New(finders.Options{Config: &model.PerRepoConfig{}}),
		Committer: &recordingCommitter{},
	})
	if err := h.LintFix(context.Background(), &command.LintFixCmd{}); err != nil {
		t.Fatalf("LintFix: %v", err)
	}

	if text := readEntryFile(t, graphDir, id); !strings.Contains(text, "kind: directive") {
		t.Errorf("kind not fixed:\n%s", text)
	}
	assertKeepsUnknownFields(t, graphDir, id)
}
