package finders_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

type recordedMoves map[string][]string

func (r recordedMoves) MoveRecordTrailers(_ context.Context, repoDir string) ([]string, error) {
	return r[repoDir], nil
}

func writeEntry(t *testing.T, dir, rel string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(path, []byte("---\ntype: signal\n---\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

// Strays are read from every former location on this machine; a former
// checkout this machine does not hold is skipped, as no work can strand
// there.
func TestGraphStrays(t *testing.T) {
	work := t.TempDir()
	current := model.GitDirLocation("graph")
	records := []model.MoveRecord{
		{ID: "a", From: model.GitPathLocation("../missing-checkout", "graph"), To: model.GitDirLocation(".sdd/graph"), Revision: "r", Entries: 1},
		{ID: "b", From: model.GitDirLocation(".sdd/graph"), To: current, Revision: "r", Entries: 1},
	}
	var trailers []string
	for _, record := range records {
		trailers = append(trailers, record.Trailers())
	}
	writeEntry(t, filepath.Join(work, "graph"), "2026/11/08-120000-s-tac-kep.md")
	writeEntry(t, filepath.Join(work, ".sdd", "graph"), "2026/11/08-120000-s-tac-kep.md")
	writeEntry(t, filepath.Join(work, ".sdd", "graph"), "2026/11/09-120000-s-tac-new.md")

	finder := finders.New(finders.Options{MoveHistory: recordedMoves{work: trailers}})
	result, err := finder.GraphStrays(t.Context(), query.GraphStraysQuery{WorkRoot: work, Repos: []string{work, work}, Current: current, GraphDir: filepath.Join(work, "graph")})
	if err != nil {
		t.Fatalf("a former checkout missing on this machine should be skipped: %v", err)
	}
	if len(result.Strays) != 1 || result.Strays[0].ID != "20261109-120000-s-tac-new" {
		t.Errorf("only the entry the graph lacks should stray, got %+v", result.Strays)
	}
	if len(result.Records) != 2 {
		t.Errorf("a repository named twice should be read once, got %d records", len(result.Records))
	}

	_, err = finders.New(finders.Options{MoveHistory: recordedMoves{work: {"sdd-move: v9\n"}}}).GraphStrays(t.Context(), query.GraphStraysQuery{WorkRoot: work, Repos: []string{work}, Current: current, GraphDir: filepath.Join(work, "graph")})
	if err == nil {
		t.Error("a move record of an unknown version should be refused")
	}
}
