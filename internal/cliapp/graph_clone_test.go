package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/test"
)

const graphCloneRepoID = model.RepoID("example.test/acme/app-sdd")

func TestMain(m *testing.M) {
	test.RunTestsAsSDD(m)
}

// graphRepoCode builds a code repository whose config names a graph
// repository with graph_repo, beside a project_dir leading nowhere, and the
// graph repository on a remote.
func graphRepoCode(t *testing.T) (code string, remote *test.GraphRemote) {
	t.Helper()
	remote = test.NewGraphRemote(t, graphCloneRepoID)
	remote.Commit(t, "seed the graph", map[string]string{
		".sdd/config.yaml":     "graph_dir: .sdd/graph\nrepo_id: " + string(graphCloneRepoID) + "\ndefault_branch: main\nlanguage: en\n",
		".sdd/graph/.keep":     "",
		".gitignore":           ".sdd/tmp/\n.sdd/config.local.yaml\n",
		"src/never-checked.go": "package code\n",
	})
	code = filepath.Join(canonicalTemp(t), "code")
	err := os.MkdirAll(code, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	initRepo(t, code)
	writeFile(t, filepath.Join(code, ".sdd", "config.yaml"), "graph_repo: "+string(graphCloneRepoID)+"\nproject_dir: ../nowhere\ngraph_dir: "+model.GraphElsewhereFile+"\nskill_scope: project\n")
	writeFile(t, filepath.Join(code, model.GraphElsewhereFile), model.GraphElsewhereText(graphCloneRepoID))
	gitIn(t, code, "add", ".sdd")
	gitIn(t, code, "commit", "-q", "-m", "point at the graph repository")
	writeFile(t, filepath.Join(code, "xdg-config", "sdd", "config.yaml"), "participant: Ada\n")
	return code, remote
}

func cloneDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.Getenv("XDG_DATA_HOME"), "sdd", "repos", filepath.FromSlash(string(graphCloneRepoID)))
}

// The first command needing the graph clones it, and a capture in the code
// repository lands in the clone, is pushed, and leaves the code repository
// untouched, though project_dir is set as well.
func TestGraphRepoCaptureGoesThroughTheClone(t *testing.T) {
	code, remote := graphRepoCode(t)
	codeHead := headSubject(t, code)

	out, err := runAppErr(t, code, "repo", "path", string(graphCloneRepoID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != cloneDir(t) {
		t.Errorf("repo path should print the clone's place under the data dir, got %q", out)
	}

	captureSignal(t, code)

	clone := cloneDir(t)
	entries := graphEntries(t, filepath.Join(clone, ".sdd", "graph"))
	if len(entries) != 1 {
		t.Fatalf("the capture should land in the clone's graph, got %v", entries)
	}
	_, statErr := os.Stat(filepath.Join(clone, "src"))
	if statErr == nil {
		t.Error("the clone should check out only .sdd")
	}
	_, pushed := remote.Show(t, ".sdd/graph/"+filepath.ToSlash(entries[0]))
	if !pushed {
		t.Error("the capture should be pushed to the graph repository")
	}
	if headSubject(t, code) != codeHead {
		t.Error("the code repository should get no commit")
	}
	if status := gitIn(t, code, "status", "--porcelain", "--untracked-files=all", "--", ".sdd"); strings.TrimSpace(status) != "" {
		t.Errorf("the code repository's .sdd should stay untouched:\n%s", status)
	}
}

// Upstream commits nobody made through sdd, touching code beside the graph,
// still merge into the clone: sync never holds there.
func TestGraphRepoSyncMergesEveryUpstreamCommit(t *testing.T) {
	code, remote := graphRepoCode(t)
	captureSignal(t, code)
	handEntry := ".sdd/graph/2026/11/07-150000-s-tac-hnd.md"
	remote.Commit(t, "a person's own commit", map[string]string{
		handEntry:          "---\ntype: signal\nlayer: tactical\nkind: gap\nconfidence: high\nparticipants: [Bo]\nsummary: Written in a person's own clone.\n---\n\nBy hand.\n",
		"src/elsewhere.go": "package code\n",
	})

	runApp(t, code, "sync")

	_, statErr := os.Stat(filepath.Join(cloneDir(t), filepath.FromSlash(handEntry)))
	if statErr != nil {
		t.Errorf("the upstream entry should arrive in the clone: %v", statErr)
	}
}
