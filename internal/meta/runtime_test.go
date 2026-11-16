package meta_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// The runtime folder ignores itself, so git sees nothing of it in any
// layout, with no ignore line naming the graph directory.
func TestEnsureRuntimeDirKeepsTheFolderOutOfGit(t *testing.T) {
	repo := t.TempDir()
	out, err := exec.Command("git", "init", "-q", repo).CombinedOutput()
	if err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	dir, err := meta.EnsureRuntimeDir(filepath.Join(repo, "decisions"))
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(repo, "decisions", model.GraphRuntimeDir) {
		t.Errorf("the runtime folder should sit in the graph directory, got %s", dir)
	}
	err = os.WriteFile(filepath.Join(dir, "graph.lock"), nil, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	status, err := exec.Command("git", "-C", repo, "status", "--porcelain", "--untracked-files=all").CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, status)
	}
	if strings.TrimSpace(string(status)) != "" {
		t.Errorf("git should see nothing of the runtime folder:\n%s", status)
	}
}
