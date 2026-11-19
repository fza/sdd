package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/index"
	localadapter "github.com/networkteam/sdd/pkg/local"
	"github.com/networkteam/sdd/test"
)

// sessionStore is the machine-wide session store sdd keeps for the checkout
// at root under its path alone.
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
	test.NewGraphRemote(t, graphCloneRepoID)
	// The state home runApp gives every command run in repo.
	t.Setenv("XDG_STATE_HOME", filepath.Join(repo, "xdg-state"))
	before := sessionStore(t, repo)
	writeFile(t, filepath.Join(before.Sessions, "s_open.jsonl"), "{\"type\":\"opened\"}\n")
	writeFile(t, filepath.Join(before.StagedBlobs, "s_open", "blob"), "staged\n")

	runApp(t, repo, "move", "--graph-repo", "https://example.test/acme/app-sdd.git")

	after := sessionStore(t, cloneDir(t))
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
	remote := test.NewGraphRemote(t, graphCloneRepoID)
	runApp(t, repo, "move", "--graph-repo", "https://example.test/acme/app-sdd.git")
	clone := cloneDir(t)
	remote.Commit(t, "a file where the replay needs a folder", map[string]string{"graph/2026/12": "not an entry\n"})
	runApp(t, repo, "repo", "sync", string(graphCloneRepoID))
	if blocker := readText(t, filepath.Join(clone, "graph", "2026", "12")); blocker != "not an entry\n" {
		t.Fatalf("the clone should hold the upstream file before the replay, got %q", blocker)
	}

	const stray = "20261205-100000-s-tac-old"
	writeFile(t, entryPath(filepath.Join(repo, ".sdd", "graph"), stray), movedEntry("Written by an older build."))
	gitIn(t, repo, "add", "--force", ".sdd/graph")
	gitIn(t, repo, "commit", "-q", "-m", "an older build captures in the code repository")
	head := gitIn(t, clone, "rev-parse", "HEAD")

	err := runAppResult(t, repo, "move", "--graph-repo", "https://example.test/acme/app-sdd.git")
	if err == nil || !strings.Contains(err.Error(), "graph/2026/12") {
		t.Fatalf("the failed replay should fail the move, naming the conflict, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(clone, ".git", "rebase-apply")); statErr == nil {
		t.Error("the failed replay should be aborted")
	}
	if status := strings.TrimSpace(gitIn(t, clone, "status", "--porcelain")); status != "" {
		t.Errorf("the failed replay should leave the target clean:\n%s", status)
	}
	if now := gitIn(t, clone, "rev-parse", "HEAD"); now != head {
		t.Error("the failed replay should commit nothing in the target")
	}
}

// A graph keeping a repo_id finds its sessions and its search index under
// the identity it had before the move, read before the move rewrites its
// config.
func TestMoveCarriesTheStoresOfAGraphWithARepoID(t *testing.T) {
	repo := graphRepoToMove(t)
	const repoID = "example.test/acme/app"
	configPath := filepath.Join(repo, ".sdd", "config.yaml")
	writeFile(t, configPath, readText(t, configPath)+"repo_id: "+repoID+"\n")
	gitIn(t, repo, "commit", "-q", "-am", "record the repository identity")
	test.NewGraphRemote(t, graphCloneRepoID)
	t.Setenv("XDG_STATE_HOME", filepath.Join(repo, "xdg-state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(repo, "xdg-cache"))
	locations := machineLocations(t)
	before, err := localadapter.SessionLocations(locations.StateRoot, filepath.Join(repo, ".sdd"), repoID, stableRoot(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(before[0].Sessions, "s_open.jsonl"), "{}\n")
	store := index.StoreDir(locations.CacheRoot, repoID, "embedder")
	writeFile(t, filepath.Join(store, "manifest.json"), "{}\n")

	runApp(t, repo, "move", "--graph-repo", "https://example.test/acme/app-sdd.git")

	clone := filepath.Join(dataHome(repo), "sdd", "repos", filepath.FromSlash(string(graphCloneRepoID)))
	after, err := localadapter.SessionLocations(locations.StateRoot, filepath.Join(clone, ".sdd"), graphCloneRepoID, stableRoot(t, clone))
	if err != nil {
		t.Fatal(err)
	}
	if session := readText(t, filepath.Join(after[0].Sessions, "s_open.jsonl")); session != "{}\n" {
		t.Errorf("the session should be found under the graph repository's identity, got %q", session)
	}
	if index.RealStoreDir(index.StoreDir(locations.CacheRoot, string(graphCloneRepoID), "embedder")) != index.RealStoreDir(store) {
		t.Error("the graph repository's identity should reach the search index store the graph had")
	}
}
