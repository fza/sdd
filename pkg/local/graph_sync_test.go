package local_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/local"
)

// syncedRepository is a checkout of main tracking a bare remote, its sync
// mode set in config.local.yaml and every file committed but the ignored ones.
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
		return pkgllm.Result{Text: "Rendered summary."}, nil
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

func TestEngineCapturePublishesUnderSyncMode(t *testing.T) {
	for _, tc := range []struct {
		mode      string
		published bool
	}{
		{mode: "auto", published: true},
		{mode: "manual"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			repo, remote := syncedRepository(t, tc.mode)
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
				t.Error("the capture commit should reach upstream exactly when sync is auto")
			}
		})
	}
}

func TestEngineCaptureRecordsARejectedPush(t *testing.T) {
	repo, remote := syncedRepository(t, "auto")
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

func TestSyncGraphPullsUpstreamGraphCommits(t *testing.T) {
	for _, tc := range []struct {
		mode   string
		pulled bool
	}{
		{mode: "auto", pulled: true},
		{mode: "manual"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			repo, remote := syncedRepository(t, tc.mode)
			upstream := pushUpstream(t, remote, ".sdd/graph/upstream.md", "upstream\n", "sdd: capture upstream")

			if err := repo.targets().SyncGraph(t.Context(), "main", sdd.SyncOnCall); err != nil {
				t.Fatal(err)
			}
			if pulled := repo.git("rev-parse", "HEAD") == upstream; pulled != tc.pulled {
				t.Error("upstream graph commits should arrive exactly when sync is auto")
			}
		})
	}
}

func TestSyncGraphWaitsOutTheCooldownOnCallsOnly(t *testing.T) {
	for _, tc := range []struct {
		trigger sdd.SyncTrigger
		pulled  bool
	}{
		{trigger: sdd.SyncOnCall},
		{trigger: sdd.SyncOnTimer, pulled: true},
	} {
		t.Run(string(tc.trigger), func(t *testing.T) {
			repo, remote := syncedRepository(t, "auto")
			repo.write(".sdd/config.local.yaml", "sync:\n  mode: auto\n  cooldown: 1h\n")
			repo.write(".sdd/tmp/last-fetch", "")
			upstream := pushUpstream(t, remote, ".sdd/graph/upstream.md", "upstream\n", "sdd: capture upstream")

			if err := repo.targets().SyncGraph(t.Context(), "main", tc.trigger); err != nil {
				t.Fatal(err)
			}
			if pulled := repo.git("rev-parse", "HEAD") == upstream; pulled != tc.pulled {
				t.Error("a call inside the cooldown should not fetch, a timer round always should")
			}
		})
	}
}

func TestSyncGraphPausesOnAConflictUntilMergedByHand(t *testing.T) {
	repo, remote := syncedRepository(t, "auto")
	source := pushUpstream(t, remote, ".sdd/graph/shared.md", "theirs\n", "sdd: edit elsewhere")
	repo.write(".sdd/graph/shared.md", "ours\n")
	repo.git("add", ".sdd/graph/shared.md")
	repo.git("commit", "-m", "sdd: edit here")
	destination := repo.git("rev-parse", "HEAD")

	if err := repo.targets().SyncGraph(t.Context(), "main", sdd.SyncOnCall); err != nil {
		t.Fatalf("a conflict should be recorded, not fail the round: %v", err)
	}
	if repo.git("rev-parse", "HEAD") != destination || repo.git("status", "--porcelain") != "" {
		t.Error("a conflict should leave the branch and the working tree as they were")
	}
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

	pushUpstream(t, remote, ".sdd/graph/later.md", "later\n", "sdd: capture later")
	tracked := repo.git("rev-parse", "origin/main")
	if err := repo.targets().SyncGraph(t.Context(), "main", sdd.SyncOnTimer); err != nil {
		t.Fatal(err)
	}
	if repo.git("rev-parse", "origin/main") != tracked {
		t.Error("a paused round should not fetch")
	}

	repo.git("merge", "--no-edit", "-m", "sdd: merge origin/main", "-X", "ours", "origin/main")
	if err := repo.targets().SyncGraph(t.Context(), "main", sdd.SyncOnTimer); err != nil {
		t.Fatal(err)
	}
	if notice, _ := repo.targets().SyncNotice(t.Context(), "main", "s_owner"); notice != "" {
		t.Errorf("the merge by hand should clear the record, got %q", notice)
	}
	if gitIn(t, "", "-C", remote, "rev-parse", "main") != repo.git("rev-parse", "HEAD") {
		t.Error("sync should resume and publish once the conflict is merged")
	}
}

func TestEngineCaptureCommitsLocallyWhilePaused(t *testing.T) {
	repo, remote := syncedRepository(t, "auto")
	pushUpstream(t, remote, ".sdd/graph/shared.md", "theirs\n", "sdd: edit elsewhere")
	repo.write(".sdd/graph/shared.md", "ours\n")
	repo.git("add", ".sdd/graph/shared.md")
	repo.git("commit", "-m", "sdd: edit here")
	if err := repo.targets().SyncGraph(t.Context(), "main", sdd.SyncOnCall); err != nil {
		t.Fatal(err)
	}
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
	repo, remote := syncedRepository(t, "auto")
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
