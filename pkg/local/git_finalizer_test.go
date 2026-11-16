package local_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/local"
)

func TestGitFinalizerCommitsBatchOnce(t *testing.T) {
	repo := newGitRepository(t)
	const entryPath = "2026/07/13-120000-s-tac-api.md"
	const attachmentPath = "2026/07/13-120000-s-tac-api/evidence.md"
	repo.write(".sdd/graph/"+entryPath, "entry\n")
	repo.write(".sdd/graph/"+attachmentPath, "evidence\n")
	finalizer := local.GitFinalizer{Checkout: repo.root, GraphDir: ".sdd/graph", Branch: "main"}
	mutation := sdd.AppliedMutation{
		BatchID: "mutation-1",
		Batch: sdd.MutationBatch{
			Message: "sdd: signal tactical captured",
			Changes: []sdd.DocumentChange{{LogicalPath: entryPath}},
			Attachments: []sdd.AttachmentMaterialization{
				{LogicalPath: attachmentPath},
				{LogicalPath: attachmentPath},
			},
		},
	}
	for range 2 {
		if err := finalizer.Finalize(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}
	}
	if commits := repo.git("log", "--fixed-strings", "--grep=SDD-Mutation: mutation-1", "--format=%H"); len(strings.Fields(commits)) != 1 {
		t.Fatalf("matching commits = %q", commits)
	}
}

// A retry whose commit landed in a graph clone without being recorded
// records it, so the clone upkeep keeps the capture.
func TestGitFinalizerRecordsALandedCommitOnRetry(t *testing.T) {
	clone, _ := syncedClone(t)
	const entryPath = "2026/11/07-120000-s-tac-api.md"
	clone.write(".sdd/graph/"+entryPath, "entry\n")
	clone.git("add", ".sdd/graph/"+entryPath)
	clone.git("commit", "-q", "-m", "sdd: signal tactical captured\n\nSDD-Mutation: mutation-1")
	finalizer := local.GitFinalizer{Checkout: clone.root, GraphDir: ".sdd/graph", Branch: "main"}
	mutation := sdd.AppliedMutation{BatchID: "mutation-1", Batch: sdd.MutationBatch{Changes: []sdd.DocumentChange{{LogicalPath: entryPath}}}}

	for range 2 {
		if err := finalizer.Finalize(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}
	}
	recorded, err := os.ReadFile(filepath.Join(clone.root, ".git", model.GraphCloneCommits))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(recorded), clone.git("rev-parse", "HEAD")); n != 1 {
		t.Errorf("the landed commit should be recorded once, %d times", n)
	}
	if err := (git.CLI{Dir: clone.root}).KeepGraphClone(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if !clone.hasPath("main", ".sdd/graph/"+entryPath) {
		t.Error("the upkeep should keep the capture the retry found")
	}
}
