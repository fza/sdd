package syncstate_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/syncstate"
)

// graphRepo is a repository with a committed graph directory.
func graphRepo(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".sdd", "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".sdd", "graph", ".keep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"}, {"add", "."},
		{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-q", "-m", "test: add graph"},
	} {
		run(t, root, args...)
	}
	return root
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func TestLockIsSharedAndRecordIsPerBranch(t *testing.T) {
	root := graphRepo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	run(t, root, "worktree", "add", "-q", "-b", "side", linked)

	_, main, err := syncstate.Open(t.Context(), filepath.Join(root, ".sdd", "graph"))
	if err != nil {
		t.Fatal(err)
	}
	_, side, err := syncstate.Open(t.Context(), filepath.Join(linked, ".sdd", "graph"))
	if err != nil {
		t.Fatal(err)
	}

	release, err := main.Lock(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	held, err := side.TryLock(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if held != nil {
		held()
		t.Error("a worktree should find the lock its sibling holds")
	}
	release()
	free, err := side.TryLock(t.Context())
	if err != nil || free == nil {
		t.Fatalf("a released lock should be free to the sibling worktree: %v", err)
	}
	free()

	block := model.SyncBlocked{Kind: model.SyncBlockedDirty, Since: time.Now().UTC().Truncate(time.Second)}
	if err := main.Update(func(*model.SyncBlocked) *model.SyncBlocked { return &block }); err != nil {
		t.Fatal(err)
	}
	if got, _ := side.Load(); got != nil {
		t.Errorf("another branch's worktree should not read the record, got %+v", got)
	}
	if err := side.Update(func(*model.SyncBlocked) *model.SyncBlocked { return nil }); err != nil {
		t.Fatal(err)
	}
	_, again, err := syncstate.Open(t.Context(), filepath.Join(root, ".sdd", "graph"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := again.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Kind != block.Kind || !got.Since.Equal(block.Since) {
		t.Errorf("a round on another branch should leave the record alone, and every process on the branch should read it, got %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "sdd", "sync.lock")); err != nil {
		t.Errorf("the lock should live in the common git directory: %v", err)
	}
}

func TestLockWaitsUntilReleased(t *testing.T) {
	_, state, err := syncstate.Open(t.Context(), filepath.Join(graphRepo(t), ".sdd", "graph"))
	if err != nil {
		t.Fatal(err)
	}
	release, err := state.Lock(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := state.Lock(ctx); err == nil {
		t.Error("a held lock should keep a waiter out until its context ends")
	}
	release()
	again, err := state.Lock(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	again()
}

func TestOpenRefusesAMissingGraphDirectory(t *testing.T) {
	_, _, err := syncstate.Open(t.Context(), filepath.Join(graphRepo(t), "missing"))
	if !errors.Is(err, git.ErrGraphRepoMissing) {
		t.Errorf("a missing graph directory should never resolve to its parent's repository, got %v", err)
	}
}

func TestTryLockSkipsAHeldGraphLock(t *testing.T) {
	graphDir := filepath.Join(graphRepo(t), ".sdd", "graph")
	_, state, err := syncstate.Open(t.Context(), graphDir)
	if err != nil {
		t.Fatal(err)
	}
	graphLock, err := syncstate.LockGraph(graphDir)
	if err != nil {
		t.Fatal(err)
	}
	release, err := state.TryLock(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if release != nil {
		release()
		t.Error("a round should skip while a commit holds the graph lock")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := state.Lock(ctx); err == nil {
		t.Error("a waiting publish should give up on the graph lock when its context ends")
	}
	syncstate.Unlock(graphLock)
	release, err = state.TryLock(t.Context())
	if err != nil || release == nil {
		t.Fatalf("a free graph lock should be taken: %v", err)
	}
	release()
}

func TestDetachedHeadKeepsNoRecord(t *testing.T) {
	root := graphRepo(t)
	run(t, root, "checkout", "-q", "--detach")
	_, state, err := syncstate.Open(t.Context(), filepath.Join(root, ".sdd", "graph"))
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Update(func(*model.SyncBlocked) *model.SyncBlocked {
		return &model.SyncBlocked{Kind: model.SyncBlockedNoUpstream}
	}); err != nil {
		t.Fatal(err)
	}
	if got, _ := state.Load(); got != nil {
		t.Errorf("a detached HEAD should keep no record, got %+v", got)
	}
}
