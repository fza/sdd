package local_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/pkg/local"
)

// A commit outside sdd's own that modifies a committed entry is a hand edit;
// sdd's own commits, new entries and other files are not (D5).
func TestGitHandEdits_FindsCommitsOutsideSddThatModifyEntries(t *testing.T) {
	repo := newGitRepository(t)
	entry := ".sdd/graph/2026/11/01-120000-d-tac-aaa.md"
	repo.write(entry, "entry\n")
	repo.git("add", entry)
	repo.git("commit", "-q", "-m", "sdd: decision tactical captured")
	since := time.Now().Add(-time.Hour)

	repo.write(entry, "entry, edited by sdd\n")
	repo.git("commit", "-q", "-am", "sdd: summarize 20261101-120000-d-tac-aaa")
	repo.write(".sdd/graph/2026/11/02-120000-s-tac-bbb.md", "new entry\n")
	repo.write("README", "changed\n")
	repo.git("add", "-A")
	repo.git("commit", "-q", "-m", "docs: add an entry by hand and touch the readme")
	if edits := scanHandEdits(t, repo, since); len(edits) != 0 {
		t.Fatalf("edits %+v: sdd's own commits and added entries are not hand edits", edits)
	}

	repo.write(entry, "entry, edited by hand\n")
	repo.git("commit", "-q", "-am", "fix: reword an entry")
	head := repo.git("rev-parse", "HEAD")
	edits := scanHandEdits(t, repo, since)
	if len(edits) != 1 || edits[0].Entry != "20261101-120000-d-tac-aaa" || edits[0].Commit != head {
		t.Fatalf("edits %+v: the hand edit should name the entry and its commit", edits)
	}
	if late := scanHandEdits(t, repo, time.Now().Add(time.Hour)); len(late) != 0 {
		t.Fatal("edits committed before since should not count")
	}
}

func TestGitHandEdits_GraphOutsideGitHasNone(t *testing.T) {
	edits, err := local.GitHandEdits{GraphDir: filepath.Join(t.TempDir(), "graph")}.HandEdits(t.Context(), time.Time{})
	if err != nil || len(edits) != 0 {
		t.Fatalf("edits %v, err %v: a graph outside Git has no history to scan", edits, err)
	}
}

func scanHandEdits(t *testing.T, repo *gitRepository, since time.Time) []handEdit {
	t.Helper()
	edits, err := local.GitHandEdits{GraphDir: filepath.Join(repo.root, ".sdd", "graph")}.HandEdits(t.Context(), since)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]handEdit, len(edits))
	for i, edit := range edits {
		out[i] = handEdit{Entry: edit.Entry, Commit: edit.Commit}
	}
	return out
}

type handEdit struct{ Entry, Commit string }
