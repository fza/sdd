package local_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/local"
)

func TestFilesystemGraphStoreReportsTheContributionRef(t *testing.T) {
	store, err := local.NewFilesystemGraphStore(local.FilesystemGraphStoreOptions{Project: "example", GraphDir: filepath.Join(t.TempDir(), "graph"), Contribution: "origin/main"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Contribution() != "origin/main" {
		t.Errorf("a snapshot should carry its checkout's contribution ref, got %q", snapshot.Contribution())
	}
	leased, err := store.AcquireSnapshot(t.Context(), sdd.SnapshotReadQuery{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := leased.Release()
		if err != nil {
			t.Error(err)
		}
	}()
	if leased.Snapshot.Contribution() != "origin/main" {
		t.Errorf("a leased snapshot should carry its checkout's contribution ref, got %q", leased.Snapshot.Contribution())
	}
}

func TestRepositoryTargetsApplyContributionModePerCheckout(t *testing.T) {
	repo := newGitRepository(t)
	repo.write(".gitignore", ".sdd/config.local.yaml\nglobal-config.yaml\n")
	repo.write(".sdd/config.yaml", "repo_id: example\ngraph_dir: .sdd/graph\ndefault_branch: main\n")
	repo.write(".sdd/graph/.gitkeep", "")
	repo.git("add", ".gitignore", ".sdd/config.yaml", ".sdd/graph/.gitkeep")
	repo.git("commit", "-m", "test: sdd project")
	repo.write("upstream.txt", "upstream\n")
	repo.git("add", "upstream.txt")
	repo.git("commit", "-m", "test: upstream change")
	repo.git("update-ref", "refs/remotes/origin/main", "HEAD")
	worktrees := canonicalTempDir(t)
	feature, stray := filepath.Join(worktrees, "feature"), filepath.Join(worktrees, "stray")
	repo.git("worktree", "add", feature, "-b", "feat/x", "origin/main")
	repo.git("worktree", "add", stray, "-b", "stray", "HEAD~1")
	for _, checkout := range []string{feature, stray} {
		err := os.WriteFile(filepath.Join(checkout, ".sdd", "config.local.yaml"), []byte("contribution: origin/main\n"), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	repo.write("global-config.yaml", "")
	application, identity, binding := repo.captureApplication(t, pkgllm.RunnerFunc(func(context.Context, pkgllm.Request) (pkgllm.Result, error) {
		return pkgllm.Result{Text: "Rendered summary."}, nil
	}))
	capture := func(sequence uint64, branch string) error {
		_, err := application.CreateEntry(t.Context(), identity, "example", binding, sdd.EntryDraft{
			Kind: "gap", Layer: "tactical", Confidence: "high", Body: "A contributed gap.",
			EntryID:     fmt.Sprintf("20260914-02000%d-s-tac-con", sequence),
			Publication: sdd.PublicationKey{Session: binding.SessionID, Sequence: sequence, Discriminator: "new-entry"},
			Target:      sdd.MutationTarget{Project: "example", Branch: branch},
		})
		return err
	}

	if err := capture(1, "feat/x"); err != nil {
		t.Fatalf("a capture on a feature branch cut from the ref should go through: %v", err)
	}
	if subject := repo.git("log", "-1", "--format=%s", "feat/x"); !strings.HasPrefix(subject, "sdd:") {
		t.Errorf("the capture should land on the feature branch, its tip is %q", subject)
	}
	info, err := application.Info(t.Context(), identity, "example", sdd.InfoRequest{Branch: "feat/x"})
	if err != nil {
		t.Fatal(err)
	}
	if info.Contribution != "origin/main" {
		t.Errorf("a read of the feature worktree should name its contribution ref, got %q", info.Contribution)
	}
	err = capture(2, "stray")
	if !errors.Is(err, model.ErrContributionRefused) || !strings.Contains(err.Error(), "stray is not cut from origin/main") {
		t.Errorf("a target worktree in contribution mode should be judged by its own config, got %v", err)
	}
	if err := capture(3, "main"); err != nil {
		t.Errorf("a checkout without the contribution key should write as before, got %v", err)
	}
	repo.write(".sdd/config.local.yaml", "contribution: origin/main\n")
	err = capture(4, "main")
	if !errors.Is(err, model.ErrContributionRefused) || !strings.Contains(err.Error(), "main is the default branch") {
		t.Errorf("the default branch should be refused in contribution mode, got %v", err)
	}
}
