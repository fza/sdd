package local_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/local"
)

// runAsHook sets the repository variables git gives a hook in hook and enters
// it, as sdd started from that hook inherits them; the returned func unsets
// them before the test reads its repositories.
func runAsHook(t *testing.T, hook *gitRepository) func() {
	t.Helper()
	t.Chdir(hook.root)
	vars := map[string]string{
		"GIT_DIR":        filepath.Join(hook.root, ".git"),
		"GIT_WORK_TREE":  hook.root,
		"GIT_INDEX_FILE": filepath.Join(hook.root, ".git", "index"),
		"GIT_COMMON_DIR": filepath.Join(hook.root, ".git"),
	}
	for name, value := range vars {
		t.Setenv(name, value)
	}
	return func() {
		for name := range vars {
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// A capture started from a git hook must still land in the store's checkout.
func TestGitPublicationReachesItsCheckoutFromAHook(t *testing.T) {
	hook := newGitRepository(t)
	repo := newGitRepository(t)
	hookHead := hook.git("rev-parse", "HEAD")
	key := sdd.PublicationKey{Session: "session", Sequence: 1, Discriminator: "new-entry"}
	const entryID = "20260914-020000-s-tac-hok"
	batch := captureBatch(t, key, entryID, "Captured from a hook.")

	leave := runAsHook(t, hook)
	store := repo.graphStore()
	_, publishErr := store.PublishEntry(t.Context(), key, batch, nil)
	finalizeErr := repo.finalizer().Finalize(t.Context(), appliedCapture(batch))
	lookup, found, lookupErr := store.LookupEntryPublication(t.Context(), key, entryID)
	leave()

	if publishErr != nil {
		t.Fatalf("PublishEntry: %v", publishErr)
	}
	if finalizeErr != nil {
		t.Fatalf("Finalize should commit in the store's checkout: %v", finalizeErr)
	}
	if message := repo.git("log", "-1", "--format=%B"); !strings.Contains(message, "SDD-Mutation: "+key.String()) {
		t.Errorf("the store's checkout should carry the publication commit, HEAD message is %q", message)
	}
	if lookupErr != nil || !found || lookup.Revision != repo.git("rev-parse", "HEAD") {
		t.Errorf("LookupEntryPublication should read the store's checkout: %+v, found=%v, err=%v", lookup, found, lookupErr)
	}
	if got := hook.git("rev-parse", "HEAD"); got != hookHead {
		t.Errorf("the hook's repository should gain no commit, HEAD moved to %q", got)
	}
	if status := hook.git("status", "--porcelain"); status != "" {
		t.Errorf("the hook's repository should stay clean, status:\n%s", status)
	}
}

// A server started from a git hook must still resolve its own checkout's
// branch and registered worktrees.
func TestGitWorktreeAcquirerReadsItsCheckoutFromAHook(t *testing.T) {
	hook := newGitRepository(t)
	repo := newGitRepository(t)
	repo.git("checkout", "-q", "-b", "graph-line")
	repo.git("worktree", "add", "-q", "-b", "feature", filepath.Join(canonicalTempDir(t), "feature"))
	acquirer, err := local.NewGitWorktreeAcquirer(local.GitWorktreeAcquirerOptions{
		Project: "example", ServerCheckout: repo.root,
		Factory: func(context.Context, string, sdd.MutationTarget) (sdd.GraphStore, []sdd.MutationFinalizer, func() error, error) {
			t.Fatal("validation must not open a target runtime")
			return nil, nil, nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	leave := runAsHook(t, hook)
	base, baseErr := acquirer.BaseBranch(t.Context())
	validateErr := acquirer.ValidateBranch(t.Context(), sdd.MutationTarget{Project: "example", Branch: "feature"})
	leave()

	if baseErr != nil || base != "graph-line" {
		t.Errorf("BaseBranch should read the serving checkout: got %q (%v)", base, baseErr)
	}
	if validateErr != nil {
		t.Errorf("ValidateBranch should find the worktree registered in the serving checkout: %v", validateErr)
	}
}
