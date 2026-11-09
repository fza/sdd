package local_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// holdIndexLock creates the repository's index lock as another git process
// would, and removes it after a short delay.
func (r *gitRepository) holdIndexLock() <-chan error {
	r.t.Helper()
	lock := filepath.Join(r.root, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		r.t.Fatal(err)
	}
	released := make(chan error, 1)
	go func() {
		time.Sleep(400 * time.Millisecond)
		released <- os.Remove(lock)
	}()
	return released
}

func capturedMutation(batchID string, changes ...sdd.DocumentChange) sdd.AppliedMutation {
	return sdd.AppliedMutation{BatchID: batchID, Batch: sdd.MutationBatch{Message: "sdd: signal tactical captured", Changes: changes}}
}

func (r *gitRepository) assertCommitted(batchID string) {
	r.t.Helper()
	if commits := r.git("log", "--fixed-strings", "--grep=SDD-Mutation: "+batchID, "--format=%H"); len(strings.Fields(commits)) != 1 {
		r.t.Fatalf("matching commits = %q", commits)
	}
}

// Another process holding the index lock while the finalizer stages an entry
// delays the capture instead of failing it.
func TestGitFinalizerWaitsOutAHeldIndexLockWhileStaging(t *testing.T) {
	repo := newGitRepository(t)
	const entryPath = "2026/07/13-120000-s-tac-api.md"
	repo.write(".sdd/graph/"+entryPath, "entry\n")
	released := repo.holdIndexLock()

	if err := repo.finalizer().Finalize(t.Context(), capturedMutation("staged", sdd.DocumentChange{LogicalPath: entryPath})); err != nil {
		t.Fatalf("finalizing should succeed once the index lock clears: %v", err)
	}
	if err := <-released; err != nil {
		t.Fatal(err)
	}
	repo.assertCommitted("staged")
}

// The same holds when the first index write is the removal of a deleted entry.
func TestGitFinalizerWaitsOutAHeldIndexLockWhileRemoving(t *testing.T) {
	repo := newGitRepository(t)
	const wipPath = "wip/20260713-120000-marker.md"
	repo.write(".sdd/graph/"+wipPath, "marker\n")
	repo.git("add", ".sdd/graph/"+wipPath)
	repo.git("commit", "-m", "test: seed marker")
	if err := os.Remove(filepath.Join(repo.root, ".sdd", "graph", filepath.FromSlash(wipPath))); err != nil {
		t.Fatal(err)
	}
	released := repo.holdIndexLock()

	if err := repo.finalizer().Finalize(t.Context(), capturedMutation("removed", sdd.DocumentChange{LogicalPath: wipPath, Delete: true})); err != nil {
		t.Fatalf("finalizing should succeed once the index lock clears: %v", err)
	}
	if err := <-released; err != nil {
		t.Fatal(err)
	}
	repo.assertCommitted("removed")
}

// A lock taken by another process between staging and committing delays the
// commit too. The post-index-change hook runs once staging has released the
// index, so it takes the lock exactly in that window, once.
func TestGitFinalizerWaitsOutAnIndexLockTakenBeforeTheCommit(t *testing.T) {
	repo := newGitRepository(t)
	const entryPath = "2026/07/13-120000-s-tac-api.md"
	repo.write(".sdd/graph/"+entryPath, "entry\n")
	lock := filepath.Join(repo.root, ".git", "index.lock")
	taken := filepath.Join(repo.root, ".git", "lock-taken")
	repo.write(".git/hooks/post-index-change", fmt.Sprintf("#!/bin/sh\n[ -e %[1]q ] && exit 0\n: > %[1]q\n: > %[2]q\n(sleep 0.4; rm -f %[2]q) >/dev/null 2>&1 &\n", taken, lock))
	if err := os.Chmod(filepath.Join(repo.root, ".git", "hooks", "post-index-change"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := repo.finalizer().Finalize(t.Context(), capturedMutation("committed", sdd.DocumentChange{LogicalPath: entryPath})); err != nil {
		t.Fatalf("finalizing should succeed once the index lock clears: %v", err)
	}
	if _, err := os.Stat(taken); err != nil {
		t.Fatalf("the hook should have taken the index lock before the commit: %v", err)
	}
	repo.assertCommitted("committed")
}

// A failure unrelated to the index lock is not retried.
func TestGitFinalizerFailsAtOnceOnOtherErrors(t *testing.T) {
	repo := newGitRepository(t)
	const entryPath = "2026/07/13-120000-s-tac-api.md"
	repo.write(".sdd/graph/"+entryPath, "entry\n")
	repo.failCommits()
	finalizer := repo.finalizer()
	finalizer.Timeout = 20 * time.Second

	start := time.Now()
	err := finalizer.Finalize(t.Context(), capturedMutation("rejected", sdd.DocumentChange{LogicalPath: entryPath}))
	if err == nil {
		t.Fatal("a commit the pre-commit hook rejects should fail")
	}
	if strings.Contains(err.Error(), "index.lock") {
		t.Fatalf("the failure should not concern the index lock: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("a failure unrelated to the index lock should return without waiting, took %s", elapsed)
	}
}
