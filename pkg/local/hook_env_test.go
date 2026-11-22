package local_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// A capture started from a git hook inherits the hook's repository variables;
// its publication must still land in the store's checkout.
func TestGitPublicationReachesItsCheckoutFromAHook(t *testing.T) {
	hook := newGitRepository(t)
	repo := newGitRepository(t)
	hookHead := hook.git("rev-parse", "HEAD")
	key := sdd.PublicationKey{Session: "session", Sequence: 1, Discriminator: "new-entry"}
	const entryID = "20260914-020000-s-tac-hok"
	batch := captureBatch(t, key, entryID, "Captured from a hook.")

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
	store := repo.graphStore()
	_, publishErr := store.PublishEntry(t.Context(), key, batch, nil)
	finalizeErr := repo.finalizer().Finalize(t.Context(), appliedCapture(batch))
	lookup, found, lookupErr := store.LookupEntryPublication(t.Context(), key, entryID)
	for name := range vars {
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}

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
