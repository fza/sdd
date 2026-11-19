package local_test

import (
	"path/filepath"
	"testing"

	app "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/local"
)

// The signature programs built on the upstream packages call; a change fails
// this file's compilation.
var _ func(app.ProjectID, string, string) (*local.GitWorktreeAcquirer, error) = local.NewRepositoryTargets

func TestRepositoryTargetsInTheDefaultLocationsReadTheCheckout(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	repo := newGitRepository(t)
	repo.write(".sdd/config.yaml", "repo_id: example\ndefault_branch: main\n")
	repo.write("global-config.yaml", "")

	targets, err := local.NewRepositoryTargets("example", repo.root, filepath.Join(repo.root, "global-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	read, err := targets.AcquireSnapshot(t.Context(), app.SnapshotReadQuery{Branch: "main"})
	if err != nil {
		t.Fatalf("a read should resolve the checkout's project: %v", err)
	}
	if err := read.Release(); err != nil {
		t.Fatal(err)
	}
}
