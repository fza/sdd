package local_test

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/syncstate"
	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/local"
	"github.com/networkteam/sdd/test"
)

// syncedRepository is a checkout of main tracking a bare remote, its sync
// mode set in config.local.yaml and every file committed but the ignored ones.
// The mode no longer reaches sync: a graph inside the code repository never
// syncs automatically.
func syncedRepository(t *testing.T, mode string) (*gitRepository, string) {
	t.Helper()
	repo := newGitRepository(t)
	remote := filepath.Join(canonicalTempDir(t), "remote.git")
	gitIn(t, "", "init", "-q", "--bare", "-b", "main", remote)
	repo.write(".sdd/config.yaml", "repo_id: example\ngraph_dir: .sdd/graph\ndefault_branch: main\n")
	repo.write(".gitignore", "global-config.yaml\n.sdd/tmp/\n.sdd/config.local.yaml\n.sdd/graph/.sdd-runtime/\n")
	repo.write("global-config.yaml", "")
	repo.write(".sdd/config.local.yaml", "sync:\n  mode: "+mode+"\n  cooldown: 1ns\n")
	repo.write(".sdd/graph/.keep", "")
	repo.git("add", ".sdd/config.yaml", ".gitignore", ".sdd/graph/.keep")
	repo.git("commit", "-m", "test: configure sdd")
	repo.git("remote", "add", "origin", remote)
	repo.git("push", "-q", "-u", "origin", "main")
	return repo, remote
}

const cloneRepoID = model.RepoID("example.test/acme/example-sdd")

// syncedClone is the sdd-owned clone of a graph repository whose remote the
// test stands in for, made the way sdd makes it: the one place sync runs
// automatically. It returns the clone and the bare remote.
func syncedClone(t *testing.T, files map[string]string) (*gitRepository, string) {
	t.Helper()
	remote := test.NewGraphRemote(t, cloneRepoID)
	seed := map[string]string{
		".sdd/config.yaml": "repo_id: example\ngraph_dir: .sdd/graph\ndefault_branch: main\n",
		".sdd/graph/.keep": "",
		".gitignore":       ".sdd/tmp/\n.sdd/config.local.yaml\n.sdd/graph/.sdd-runtime/\n",
	}
	maps.Copy(seed, files)
	remote.Commit(t, "sdd: init", seed)
	gate := gitgate.New(gitgate.Options{StateDir: t.TempDir(), Limits: func(string) gitgate.Limits { return model.DefaultGitLimits() }})
	dir, _, err := repos.NewGraphClones(repos.Locations{DataRoot: canonicalTempDir(t)}, git.GraphClones{}, gate).Ensure(t.Context(), cloneRepoID, "")
	if err != nil {
		t.Fatal(err)
	}
	return &gitRepository{t: t, root: dir}, remote.Bare
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func summaryRunner() pkgllm.Runner {
	return pkgllm.RunnerFunc(func(context.Context, pkgllm.Request) (pkgllm.Result, error) {
		return pkgllm.Result{Text: `{"title": "test title", "summary": "Rendered summary."}`}, nil
	})
}

func captureDraft(binding sdd.SessionBinding, entryID string) sdd.EntryDraft {
	return sdd.EntryDraft{
		Kind: "gap", Layer: "tactical", Confidence: "high", Body: "Engine capture under sync.",
		EntryID: entryID, Publication: sdd.PublicationKey{Session: binding.SessionID, Sequence: 1, Discriminator: "new-entry"},
	}
}

func rejectPushes(t *testing.T, remote string) func() {
	t.Helper()
	hook := filepath.Join(remote, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := os.Remove(hook); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEngineCapturePublishesOnlyFromAGraphClone(t *testing.T) {
	for _, tc := range []struct {
		name      string
		checkout  func(t *testing.T) (*gitRepository, string)
		published bool
	}{
		{name: "graph clone", checkout: func(t *testing.T) (*gitRepository, string) { return syncedClone(t, nil) }, published: true},
		{name: "code repository set to auto", checkout: func(t *testing.T) (*gitRepository, string) { return syncedRepository(t, "auto") }},
		{name: "code repository set to manual", checkout: func(t *testing.T) (*gitRepository, string) { return syncedRepository(t, "manual") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, remote := tc.checkout(t)
			before := gitIn(t, "", "-C", remote, "rev-parse", "main")
			application, identity, binding := repo.captureApplication(t, summaryRunner())
			if _, err := application.CreateEntry(t.Context(), identity, "example", binding, captureDraft(binding, "20261027-200000-s-tac-syn")); err != nil {
				t.Fatal(err)
			}
			want := before
			if tc.published {
				want = repo.git("rev-parse", "HEAD")
			}
			if got := gitIn(t, "", "-C", remote, "rev-parse", "main"); got != want {
				t.Error("the capture commit should reach upstream exactly from a graph clone")
			}
		})
	}
}

func TestEngineCaptureRecordsARejectedPush(t *testing.T) {
	repo, remote := syncedClone(t, nil)
	application, identity, binding := repo.captureApplication(t, summaryRunner())

	allowPushes := rejectPushes(t, remote)
	if _, err := application.CreateEntry(t.Context(), identity, "example", binding, captureDraft(binding, "20261027-200001-s-tac-syn")); err != nil {
		t.Fatalf("a rejected push should leave the capture committed and successful: %v", err)
	}
	landed := repo.git("rev-parse", "HEAD")
	notice, err := repo.targets().SyncNotice(t.Context(), "main", "s_1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notice, "pushing to") {
		t.Errorf("the rejected push should be served as a blocked sync, got %q", notice)
	}
	allowPushes()
	if err := repo.targets().SyncGraph(t.Context(), "main", sdd.SyncOnCall); err != nil {
		t.Fatal(err)
	}
	if gitIn(t, "", "-C", remote, "rev-parse", "main") != landed {
		t.Error("the next round should publish the commit that landed")
	}
	if notice, _ := repo.targets().SyncNotice(t.Context(), "main", "s_1"); notice != "" {
		t.Errorf("a round that publishes should clear the block, got %q", notice)
	}
}

// pushUpstream commits path with content in a clone of remote and pushes it.
func pushUpstream(t *testing.T, remote, path, content, subject string) string {
	t.Helper()
	other := filepath.Join(canonicalTempDir(t), "other")
	if _, err := os.Stat(other); err != nil {
		gitIn(t, "", "clone", "-q", remote, other)
	}
	gitIn(t, other, "pull", "-q", "--no-rebase")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(other, path)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, other, "add", path)
	gitIn(t, other, "-c", "user.name=Other", "-c", "user.email=other@example.invalid", "commit", "-q", "-m", subject)
	gitIn(t, other, "push", "-q")
	return gitIn(t, other, "rev-parse", "HEAD")
}

func TestSyncGraphPullsOnlyIntoAGraphClone(t *testing.T) {
	for _, tc := range []struct {
		name     string
		checkout func(t *testing.T) (*gitRepository, string)
		pulled   bool
	}{
		{name: "graph clone", checkout: func(t *testing.T) (*gitRepository, string) { return syncedClone(t, nil) }, pulled: true},
		{name: "code repository set to auto", checkout: func(t *testing.T) (*gitRepository, string) { return syncedRepository(t, "auto") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, remote := tc.checkout(t)
			upstream := pushUpstream(t, remote, ".sdd/graph/upstream.md", "upstream\n", "sdd: capture upstream")

			if err := repo.targets().SyncGraph(t.Context(), "main", sdd.SyncOnCall); err != nil {
				t.Fatal(err)
			}
			if pulled := repo.git("rev-parse", "HEAD") == upstream; pulled != tc.pulled {
				t.Error("upstream graph commits should arrive exactly in a graph clone")
			}
		})
	}
}

func TestSyncGraphFetchesWhateverTheTrigger(t *testing.T) {
	for _, trigger := range []sdd.SyncTrigger{sdd.SyncOnCall, sdd.SyncOnTimer} {
		t.Run(string(trigger), func(t *testing.T) {
			repo, remote := syncedClone(t, nil)
			upstream := pushUpstream(t, remote, ".sdd/graph/upstream.md", "upstream\n", "sdd: capture upstream")

			if err := repo.targets().SyncGraph(t.Context(), "main", trigger); err != nil {
				t.Fatal(err)
			}
			if repo.git("rev-parse", "HEAD") != upstream {
				t.Error("the git gate, not the trigger, should decide whether a round fetches")
			}
		})
	}
}

func TestSyncDue(t *testing.T) {
	repo, _ := syncedClone(t, nil)
	due, err := repo.targets().SyncDue(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if !due {
		t.Error("a graph clone on a remote the gate always admits should be due")
	}
	code, _ := syncedRepository(t, "auto")
	due, err = code.targets().SyncDue(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if due {
		t.Error("a graph inside the code repository should never be due, whatever sync.mode says")
	}
}

// pauseOnConflict records a conflict with upstream commit source, as a round
// meeting one would.
func pauseOnConflict(t *testing.T, repo *gitRepository, source string) {
	t.Helper()
	_, state, err := syncstate.Open(t.Context(), filepath.Join(repo.root, ".sdd", "graph"))
	if err != nil {
		t.Fatal(err)
	}
	err = state.Update(func(*model.SyncBlocked) *model.SyncBlocked {
		return &model.SyncBlocked{
			Kind: model.SyncBlockedConflict, Since: time.Now(), Upstream: "origin/main", Paths: []string{".sdd/graph/shared.md"},
			Source: source, Destination: repo.git("rev-parse", "HEAD"),
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSyncGraphPausesOnAConflictUntilMerged(t *testing.T) {
	repo, remote := syncedClone(t, nil)
	source := pushUpstream(t, remote, ".sdd/graph/shared.md", "theirs\n", "sdd: edit elsewhere")
	pauseOnConflict(t, repo, source)

	owner, err := repo.targets().SyncNotice(t.Context(), "main", "s_owner")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`".sdd/graph/shared.md"`, `"sdd: merge origin/main"`, source[:12]} {
		if !strings.Contains(owner, want) {
			t.Errorf("the owner's notice should name the path, the merge message and the source commit, missing %q in %q", want, owner)
		}
	}
	other, err := repo.targets().SyncNotice(t.Context(), "main", "s_other")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(other, "sync blocked since") || strings.Contains(other, "git merge") {
		t.Errorf("another session should get the banner alone, got %q", other)
	}

	tracked := repo.git("rev-parse", "origin/main")
	if err := repo.targets().SyncGraph(t.Context(), "main", sdd.SyncOnTimer); err != nil {
		t.Fatal(err)
	}
	if repo.git("rev-parse", "origin/main") != tracked {
		t.Error("a paused round should not fetch")
	}

	repo.git("fetch", "-q", "https")
	repo.git("merge", "-q", "--no-edit", "-m", "sdd: merge origin/main", "origin/main")
	later := pushUpstream(t, remote, ".sdd/graph/later.md", "later\n", "sdd: capture later")
	if err := repo.targets().SyncGraph(t.Context(), "main", sdd.SyncOnTimer); err != nil {
		t.Fatal(err)
	}
	if notice, _ := repo.targets().SyncNotice(t.Context(), "main", "s_owner"); notice != "" {
		t.Errorf("the merge should clear the record, got %q", notice)
	}
	if repo.git("rev-parse", "HEAD") != later || gitIn(t, "", "-C", remote, "rev-parse", "main") != later {
		t.Error("sync should resume once the conflict's source is merged")
	}
}

func TestEngineCaptureCommitsLocallyWhilePaused(t *testing.T) {
	repo, remote := syncedClone(t, nil)
	pauseOnConflict(t, repo, pushUpstream(t, remote, ".sdd/graph/shared.md", "theirs\n", "sdd: edit elsewhere"))
	published := gitIn(t, "", "-C", remote, "rev-parse", "main")

	application, identity, binding := repo.captureApplication(t, summaryRunner())
	if _, err := application.CreateEntry(t.Context(), identity, "example", binding, captureDraft(binding, "20261027-200002-s-tac-syn")); err != nil {
		t.Fatalf("a capture should succeed while sync is paused: %v", err)
	}
	if gitIn(t, "", "-C", remote, "rev-parse", "main") != published {
		t.Error("a capture while paused should not push")
	}
	if !strings.Contains(repo.git("log", "-1", "--format=%s"), "sdd:") {
		t.Error("the capture should commit locally")
	}
}

func TestConcurrentSyncRoundsNeverOverlap(t *testing.T) {
	repo, remote := syncedClone(t, nil)
	for i := range 3 {
		pushUpstream(t, remote, fmt.Sprintf(".sdd/graph/upstream-%d.md", i), "upstream\n", "sdd: capture upstream")
	}
	targets := []*local.GitWorktreeAcquirer{repo.targets(), repo.targets(), repo.targets(), repo.targets()}
	var wg sync.WaitGroup
	errs := make(chan error, len(targets)*3)
	for _, target := range targets {
		wg.Go(func() {
			for range 3 {
				errs <- target.SyncGraph(t.Context(), "main", sdd.SyncOnTimer)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent rounds should take turns without a git failure: %v", err)
		}
	}
	if notice, _ := repo.targets().SyncNotice(t.Context(), "main", "s_1"); notice != "" {
		t.Errorf("concurrent rounds should leave nothing blocked, got %q", notice)
	}
	if repo.git("rev-parse", "HEAD") != gitIn(t, "", "-C", remote, "rev-parse", "main") {
		t.Error("the rounds should leave the branch at the upstream")
	}
}

func TestHeldSyncLockKeepsARoundOut(t *testing.T) {
	repo, remote := syncedClone(t, nil)
	upstream := pushUpstream(t, remote, ".sdd/graph/upstream.md", "upstream\n", "sdd: capture upstream")
	_, state, err := syncstate.Open(t.Context(), filepath.Join(repo.root, ".sdd", "graph"))
	if err != nil {
		t.Fatal(err)
	}
	release, err := state.Lock(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.targets().SyncGraph(t.Context(), "main", sdd.SyncOnTimer); err != nil {
		t.Fatal(err)
	}
	if repo.git("rev-parse", "origin/main") == upstream {
		t.Error("a round should not fetch while the sync lock is held")
	}
	release()
	if err := repo.targets().SyncGraph(t.Context(), "main", sdd.SyncOnTimer); err != nil {
		t.Fatal(err)
	}
	if repo.git("rev-parse", "origin/main") != upstream {
		t.Error("once the lock is free the round should fetch")
	}
}

func TestPublishWaitsForTheSyncLock(t *testing.T) {
	repo, remote := syncedClone(t, nil)
	before := gitIn(t, "", "-C", remote, "rev-parse", "main")
	// The sync lock alone, as another process's round holds it, never this
	// graph directory's own lock.
	syncLock := flock.New(filepath.Join(repo.root, ".git", "sdd", "sync.lock"))
	err := os.MkdirAll(filepath.Dir(syncLock.Path()), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = syncLock.Lock()
	if err != nil {
		t.Fatal(err)
	}
	release := func() { _ = syncLock.Unlock() }
	application, identity, binding := repo.captureApplication(t, summaryRunner())
	done := make(chan error, 1)
	go func() {
		_, err := application.CreateEntry(t.Context(), identity, "example", binding, captureDraft(binding, "20261028-230000-s-tac-wai"))
		done <- err
	}()
	select {
	case err := <-done:
		release()
		t.Fatalf("a publish should wait for the sync lock, returned %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	if gitIn(t, "", "-C", remote, "rev-parse", "main") != before {
		t.Error("nothing should be pushed while another process holds the sync lock")
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the publish should finish once the lock is free")
	}
	if gitIn(t, "", "-C", remote, "rev-parse", "main") != repo.git("rev-parse", "HEAD") {
		t.Error("the waiting publish should still reach the upstream")
	}
}
