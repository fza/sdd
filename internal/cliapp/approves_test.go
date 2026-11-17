package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sdd new writes a person's approval of an agent-decided entry through
// --approves, resolving its short ID like --closes.
func TestNewWritesAnApproval(t *testing.T) {
	project := canonicalTemp(t)
	initRepo(t, project)
	graph := filepath.Join(project, ".sdd", "graph")
	if err := os.MkdirAll(filepath.Join(graph, "2026", "11"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\n")
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")
	writeFile(t, filepath.Join(graph, "2026", "11", "17-090000-d-tac-agt.md"), "---\ntype: decision\nlayer: tactical\nkind: plan\ndecided-by: agent\nrun-by: Ada\nreason: The build needs it.\nconfidence: high\nsummary: An agent's plan.\n---\n\nAn agent's plan.\n\n## Acceptance criteria\n\n- [ ] It builds.\n")

	runApp(t, project,
		"new", "decision", "tactical", "--intent", "settled",
		"--approves", "d-tac-agt", "--skip-grounding",
		"--confidence", "high", "--skip-preflight",
		"--summary", "Ada approves the agent's plan.",
		"--title", "Plan approval",
		"The agent's plan holds: Ada checked it against the build.",
	)

	for _, name := range graphEntries(t, graph) {
		if strings.Contains(name, "d-tac-agt") {
			continue
		}
		if written := readFile(t, filepath.Join(graph, name)); !strings.Contains(written, "approves:\n    - 20261117-090000-d-tac-agt") {
			t.Errorf("the approval should list the resolved ID:\n%s", written)
		}
		return
	}
	t.Fatal("no approval written")
}
