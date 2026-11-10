package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofrs/flock"

	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/syncstate"
	"github.com/networkteam/sdd/test"
)

const lineageRecord = "rev-two rev-one\n"

// withRuntime gives graphDir the runtime folder a graph store leaves: its
// ignore file, both lock files and a lineage record.
func withRuntime(t *testing.T, graphDir string) {
	t.Helper()
	dir, err := meta.EnsureRuntimeDir(graphDir)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "graph.lock"), "")
	writeFile(t, filepath.Join(dir, "titles.lock"), "")
	writeFile(t, filepath.Join(dir, "lineage"), lineageRecord)
}

func assertRuntimeCarried(t *testing.T, from, to string) {
	t.Helper()
	if lineage := readText(t, filepath.Join(to, model.GraphRuntimeDir, "lineage")); lineage != lineageRecord {
		t.Errorf("the lineage record should move to %s, got %q", to, lineage)
	}
	for _, name := range []string{".gitignore", "graph.lock", "titles.lock"} {
		if _, err := os.Stat(filepath.Join(to, model.GraphRuntimeDir, name)); err != nil {
			t.Errorf("%s should move with the runtime folder: %v", name, err)
		}
	}
	for _, name := range []string{"graph.lock", "titles.lock"} {
		lock := flock.New(filepath.Join(to, model.GraphRuntimeDir, name))
		taken, err := lock.TryLock()
		if err != nil || !taken {
			t.Errorf("the move should leave %s released: taken=%v err=%v", name, taken, err)
		}
		_ = lock.Unlock()
	}
	assertMissing(t, from, "the former graph folder holding only the runtime folder")
}

// The runtime folder moves with the graph, and the former graph folder it
// leaves empty goes.
func TestMoveCarriesTheGraphsRuntimeFolder(t *testing.T) {
	repo := graphRepoToMove(t)
	withRuntime(t, filepath.Join(repo, ".sdd", "graph"))
	runApp(t, repo, "move", "--graph-dir", "graph")

	assertRuntimeCarried(t, filepath.Join(repo, ".sdd", "graph"), filepath.Join(repo, "graph"))
	if status := strings.TrimSpace(gitIn(t, repo, "status", "--porcelain")); status != "" {
		t.Errorf("the carried runtime folder should ignore itself:\n%s", status)
	}
}

func TestMoveCarriesTheRuntimeFolderAcrossRepositories(t *testing.T) {
	t.Run("project dir and back", func(t *testing.T) {
		repo := graphRepoToMove(t)
		project := filepath.Join(filepath.Dir(repo), "graph-home")
		withRuntime(t, filepath.Join(repo, ".sdd", "graph"))

		runApp(t, repo, "move", "--project-dir", "../graph-home")
		assertRuntimeCarried(t, filepath.Join(repo, ".sdd", "graph"), filepath.Join(project, "graph"))

		runApp(t, repo, "move", "--graph-dir", ".sdd/graph")
		assertRuntimeCarried(t, filepath.Join(project, "graph"), filepath.Join(repo, ".sdd", "graph"))
	})
	t.Run("graph repo and back", func(t *testing.T) {
		repo := graphRepoToMove(t)
		test.NewGraphRemote(t, graphCloneRepoID)
		withRuntime(t, filepath.Join(repo, ".sdd", "graph"))

		runApp(t, repo, "move", "--graph-repo", "https://example.test/acme/app-sdd.git")
		clone := cloneDir(t)
		assertRuntimeCarried(t, filepath.Join(repo, ".sdd", "graph"), filepath.Join(clone, "graph"))

		runApp(t, repo, "move", "--graph-dir", ".sdd/graph")
		assertRuntimeCarried(t, filepath.Join(clone, "graph"), filepath.Join(repo, ".sdd", "graph"))
	})
}

// A former graph folder holding more than the runtime folder stays, what it
// holds untouched.
func TestMoveKeepsAFormerGraphFolderHoldingMore(t *testing.T) {
	repo := graphRepoToMove(t)
	former := filepath.Join(repo, ".sdd", "graph")
	withRuntime(t, former)
	writeFile(t, filepath.Join(former, "notes", "draft.txt"), "kept\n")
	writeFile(t, filepath.Join(repo, ".gitignore"), "xdg-*/\nnotes/\n")
	gitIn(t, repo, "commit", "-q", "-m", "ignore notes", "--", ".gitignore")

	runApp(t, repo, "move", "--graph-dir", "graph")

	if draft := readText(t, filepath.Join(former, "notes", "draft.txt")); draft != "kept\n" {
		t.Errorf("the leftover file should stay untouched, got %q", draft)
	}
	if lineage := readText(t, filepath.Join(repo, "graph", model.GraphRuntimeDir, "lineage")); lineage != lineageRecord {
		t.Errorf("the runtime folder should move all the same, got %q", lineage)
	}
	assertMissing(t, filepath.Join(former, model.GraphRuntimeDir), "the runtime folder moves out")
}

// A runtime file the new graph folder holds already stays, and its namesake
// stays in the former folder.
func TestMoveKeepsARuntimeFileTheNewFolderHolds(t *testing.T) {
	repo := graphRepoToMove(t)
	former := filepath.Join(repo, ".sdd", "graph")
	withRuntime(t, former)
	_, err := meta.EnsureRuntimeDir(filepath.Join(repo, "graph"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "graph", model.GraphRuntimeDir, "lineage"), "rev-new\n")

	runApp(t, repo, "move", "--graph-dir", "graph")

	if lineage := readText(t, filepath.Join(repo, "graph", model.GraphRuntimeDir, "lineage")); lineage != "rev-new\n" {
		t.Errorf("the new folder's lineage record should stay, got %q", lineage)
	}
	if lineage := readText(t, filepath.Join(former, model.GraphRuntimeDir, "lineage")); lineage != lineageRecord {
		t.Errorf("the former lineage record should stay where it is, got %q", lineage)
	}
}

// A lock another process holds on the former graph folder keeps its lock
// file where it is, held; the rest of the runtime folder moves.
func TestMoveLeavesAHeldLockFileInPlace(t *testing.T) {
	repo := graphRepoToMove(t)
	former := filepath.Join(repo, ".sdd", "graph")
	withRuntime(t, former)
	lock, err := syncstate.LockGraph(former)
	if err != nil {
		t.Fatal(err)
	}
	defer syncstate.Unlock(lock)

	runApp(t, repo, "move", "--graph-dir", "graph")

	taken, err := flock.New(filepath.Join(former, model.GraphRuntimeDir, "graph.lock")).TryLock()
	if err != nil || taken {
		t.Errorf("the held lock file should stay where it is, still held: taken=%v err=%v", taken, err)
	}
	if _, err := os.Stat(filepath.Join(repo, "graph", model.GraphRuntimeDir, "graph.lock")); err == nil {
		t.Error("the held lock file should not be renamed into the new graph folder")
	}
	if lineage := readText(t, filepath.Join(repo, "graph", model.GraphRuntimeDir, "lineage")); lineage != lineageRecord {
		t.Errorf("the lineage record should move all the same, got %q", lineage)
	}
	if _, err := os.Stat(filepath.Join(repo, "graph", model.GraphRuntimeDir, "titles.lock")); err != nil {
		t.Errorf("the free titles lock should move: %v", err)
	}
}
