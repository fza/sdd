package local_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
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
	repo.git("add", ".sdd/config.yaml", ".gitignore")
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

func TestEngineCaptureRetryPublishesALandedCommit(t *testing.T) {
	repo, remote := syncedRepository(t, "auto")
	application, identity, binding := repo.captureApplication(t, summaryRunner())
	draft := captureDraft(binding, "20261027-200001-s-tac-syn")

	allowPushes := rejectPushes(t, remote)
	if _, err := application.CreateEntry(t.Context(), identity, "example", binding, draft); err == nil {
		t.Fatal("a rejected push should fail the capture")
	}
	landed := repo.git("rev-parse", "HEAD")
	allowPushes()
	if _, err := application.CreateEntry(t.Context(), identity, "example", binding, draft); err != nil {
		t.Fatal(err)
	}
	if repo.git("rev-parse", "HEAD") != landed {
		t.Error("the retry should reuse the commit that landed")
	}
	if gitIn(t, "", "-C", remote, "rev-parse", "main") != landed {
		t.Error("the retry should publish the commit that landed")
	}
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
			other := filepath.Join(canonicalTempDir(t), "other")
			gitIn(t, "", "clone", "-q", remote, other)
			if err := os.MkdirAll(filepath.Join(other, ".sdd", "graph"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(other, ".sdd", "graph", "upstream.md"), []byte("upstream\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitIn(t, other, "add", ".sdd/graph/upstream.md")
			gitIn(t, other, "-c", "user.name=Other", "-c", "user.email=other@example.invalid", "commit", "-q", "-m", "sdd: capture upstream")
			gitIn(t, other, "push", "-q")
			upstream := gitIn(t, other, "rev-parse", "HEAD")

			notice, err := repo.targets().SyncGraph(t.Context(), "main")
			if err != nil || notice != "" {
				t.Fatalf("a clean pull should raise no notice: %q, %v", notice, err)
			}
			if pulled := repo.git("rev-parse", "HEAD") == upstream; pulled != tc.pulled {
				t.Error("upstream graph commits should arrive exactly when sync is auto")
			}
		})
	}
}

func TestSyncGraphWaitsOutTheCooldown(t *testing.T) {
	repo, remote := syncedRepository(t, "auto")
	repo.write(".sdd/config.local.yaml", "sync:\n  mode: auto\n  cooldown: 1h\n")
	repo.write(".sdd/tmp/last-fetch", "")
	other := filepath.Join(canonicalTempDir(t), "other")
	gitIn(t, "", "clone", "-q", remote, other)
	gitIn(t, other, "-c", "user.name=Other", "-c", "user.email=other@example.invalid", "commit", "-q", "--allow-empty", "-m", "sdd: capture upstream")
	gitIn(t, other, "push", "-q")
	repo.git("fetch", "-q")
	before := repo.git("rev-parse", "HEAD")

	if _, err := repo.targets().SyncGraph(t.Context(), "main"); err != nil {
		t.Fatal(err)
	}
	if repo.git("rev-parse", "HEAD") != before {
		t.Error("a sync inside the cooldown should leave the branch as it is")
	}
}

func TestSyncGraphReturnsAConflictNotice(t *testing.T) {
	repo, remote := syncedRepository(t, "auto")
	other := filepath.Join(canonicalTempDir(t), "other")
	gitIn(t, "", "clone", "-q", remote, other)
	if err := os.MkdirAll(filepath.Join(other, ".sdd", "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, ".sdd", "graph", "shared.md"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, other, "add", ".sdd/graph/shared.md")
	gitIn(t, other, "-c", "user.name=Other", "-c", "user.email=other@example.invalid", "commit", "-q", "-m", "sdd: edit elsewhere")
	gitIn(t, other, "push", "-q")
	repo.write(".sdd/graph/shared.md", "ours\n")
	repo.git("add", ".sdd/graph/shared.md")
	repo.git("commit", "-m", "sdd: edit here")
	before := repo.git("rev-parse", "HEAD")

	notice, err := repo.targets().SyncGraph(t.Context(), "main")
	if err != nil {
		t.Fatalf("a conflict should come back as a notice, not an error: %v", err)
	}
	if !strings.Contains(notice, ".sdd/graph/shared.md") || !strings.Contains(notice, `"sdd: merge origin/main"`) {
		t.Errorf("the notice should name the conflicting path and the merge message, got %q", notice)
	}
	if repo.git("rev-parse", "HEAD") != before || repo.git("status", "--porcelain") != "" {
		t.Error("a conflict should leave the branch and the working tree as they were")
	}
}
