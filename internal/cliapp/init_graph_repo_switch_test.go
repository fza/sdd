package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/test"
)

// One `sdd init --graph-repo` run switches a code repository that points at
// its graph through project_dir, though its machine-local layer already holds
// the settings a graph_repo project keeps there (20261110-134112-d-tac-42w).
func TestInitGraphRepoSwitchesAProjectDirRepository(t *testing.T) {
	remote := test.NewGraphRemote(t, graphCloneRepoID)
	remote.Commit(t, "seed the graph", map[string]string{
		".sdd/config.yaml": "graph_dir: .sdd/graph\nrepo_id: " + string(graphCloneRepoID) + "\ndefault_branch: main\nlanguage: en\nverify:\n  mode: off\n",
		".sdd/graph/.keep": "",
		".gitignore":       ".sdd/tmp/\n.sdd/config.local.yaml\n",
	})
	checkout := remote.Clone(t)
	code := filepath.Join(canonicalTemp(t), "code")
	err := os.MkdirAll(code, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	initRepo(t, code)
	writeFile(t, filepath.Join(code, ".gitignore"), ".sdd/config.local.yaml\nxdg-*/\n")
	writeFile(t, filepath.Join(code, ".sdd", "config.yaml"), "project_dir: "+checkout+"\nskill_scope: project\nsupported_agents: [claude]\n")
	gitIn(t, code, "add", ".gitignore", ".sdd")
	gitIn(t, code, "commit", "-q", "-m", "point at the graph checkout")
	writeFile(t, filepath.Join(code, ".sdd", "config.local.yaml"), "participant: Ada\ndialogue:\n  mode: guided\n")

	runApp(t, code, "init", "--graph-repo", "https://example.test/acme/app-sdd.git")

	pointer := readFile(t, filepath.Join(code, ".sdd", "config.yaml"))
	if !strings.Contains(pointer, "graph_repo: "+string(graphCloneRepoID)) || !strings.Contains(pointer, "project_dir: "+checkout) {
		t.Errorf("the code repository should point through both pointers:\n%s", pointer)
	}
	local := readFile(t, filepath.Join(code, ".sdd", "config.local.yaml"))
	if !strings.Contains(local, "participant: Ada") || !strings.Contains(local, "mode: guided") {
		t.Errorf("the local layer should keep the person's settings:\n%s", local)
	}
}

// Init refreshes an existing clone on the remote's state: what the graph
// repository gained since the clone's last fetch arrives first.
func TestInitGraphRepoSyncsAnExistingClone(t *testing.T) {
	code, remote := graphRepoCode(t)
	captureSignal(t, code)
	upstreamEntry := ".sdd/graph/2026/11/10-120000-s-tac-up1.md"
	remote.Commit(t, "an entry from another machine", map[string]string{
		upstreamEntry: "---\ntype: signal\nlayer: tactical\nkind: gap\nconfidence: high\nparticipants: [Bo]\nsummary: Captured on another machine.\n---\n\nElsewhere.\n",
	})

	runApp(t, code, "init")

	_, statErr := os.Stat(filepath.Join(cloneDir(t), filepath.FromSlash(upstreamEntry)))
	if statErr != nil {
		t.Errorf("the upstream entry should reach the clone before the refresh: %v", statErr)
	}
}
