package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	localadapter "github.com/networkteam/sdd/pkg/local"
)

// sessionStore is the machine-wide session store the checkout at root reads
// first, its graph keeping no repo_id.
func sessionStore(t *testing.T, root string) localadapter.StoreLocation {
	t.Helper()
	stores, err := localadapter.SessionLocations(machineLocations(t).StateRoot, filepath.Join(root, ".sdd"), "", stableRoot(t, root))
	if err != nil {
		t.Fatal(err)
	}
	return stores[0]
}

// A session open in the machine-wide store before a move is found under the
// repository key the moved graph is read under, its staged blobs with it.
func TestMoveCarriesTheMachineWideSessionStores(t *testing.T) {
	repo := graphRepoToMove(t)
	// The state home runApp gives every command run in repo.
	t.Setenv("XDG_STATE_HOME", filepath.Join(repo, "xdg-state"))
	project := filepath.Join(filepath.Dir(repo), "graph-home")
	before := sessionStore(t, repo)
	writeFile(t, filepath.Join(before.Sessions, "s_open.jsonl"), "{\"type\":\"opened\"}\n")
	writeFile(t, filepath.Join(before.StagedBlobs, "s_open", "blob"), "staged\n")

	runApp(t, repo, "move", "--project-dir", "../graph-home")

	after := sessionStore(t, project)
	if after.Sessions == before.Sessions {
		t.Fatal("the moved graph should be read under another repository key")
	}
	if session := readText(t, filepath.Join(after.Sessions, "s_open.jsonl")); session != "{\"type\":\"opened\"}\n" {
		t.Errorf("the open session should be found under the new repository key, got %q", session)
	}
	if blob := readText(t, filepath.Join(after.StagedBlobs, "s_open", "blob")); blob != "staged\n" {
		t.Errorf("the session's staged blobs should follow it, got %q", blob)
	}
	assertMissing(t, filepath.Join(before.Sessions, "s_open.jsonl"), "the session moves")
	runApp(t, repo, "show", movedFirst)
}

// A replay of a stray's history that git cannot apply in the target is
// aborted: the move fails naming the conflict and leaves the target with no
// replay in progress and a clean tree.
func TestMoveAbortsAFailedReplay(t *testing.T) {
	repo := graphRepoToMove(t)
	project := filepath.Join(filepath.Dir(repo), "graph-home")
	runApp(t, repo, "move", "--project-dir", "../graph-home")

	const stray = "20261205-100000-s-tac-old"
	writeFile(t, entryPath(filepath.Join(repo, ".sdd", "graph"), stray), movedEntry("Written by an older build."))
	gitIn(t, repo, "add", "--force", ".sdd/graph")
	gitIn(t, repo, "commit", "-q", "-m", "an older build captures in the code repository")
	writeFile(t, filepath.Join(project, "graph", "2026", "12"), "not an entry\n")
	gitIn(t, project, "add", "graph/2026/12")
	gitIn(t, project, "commit", "-q", "-m", "a file where the replay needs a folder")
	head := gitIn(t, project, "rev-parse", "HEAD")

	err := runAppResult(t, repo, "move", "--project-dir", "../graph-home")
	if err == nil || !strings.Contains(err.Error(), "graph/2026/12") {
		t.Fatalf("the failed replay should fail the move, naming the conflict, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(project, ".git", "rebase-apply")); statErr == nil {
		t.Error("the failed replay should be aborted")
	}
	if status := strings.TrimSpace(gitIn(t, project, "status", "--porcelain")); status != "" {
		t.Errorf("the failed replay should leave the target clean:\n%s", status)
	}
	if now := gitIn(t, project, "rev-parse", "HEAD"); now != head {
		t.Error("the failed replay should commit nothing in the target")
	}
}
