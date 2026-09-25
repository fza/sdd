package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The flag is the only way the field gets set: it is never inferred from prose
// and never assumed from a name, so a consumer can trust what it reads.
func TestNewActorRecordsTheActorKind(t *testing.T) {
	project := canonicalTemp(t)
	initRepo(t, project)
	if err := os.MkdirAll(filepath.Join(project, ".sdd", "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\n")
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")

	runApp(t, project,
		"new", "signal", "process", "--kind", "actor",
		"--canonical", "Claude", "--actor-kind", "machine",
		"--confidence", "high", "--skip-preflight",
		"--summary", "An assistant participating in the project.",
		"Claude is an assistant working on this project; the decision behind its proposals belongs to a human participant.",
	)

	entries := graphEntries(t, filepath.Join(project, ".sdd", "graph"))
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %v", entries)
	}
	if written := readFile(t, filepath.Join(project, ".sdd", "graph", entries[0])); !strings.Contains(written, "actor_kind: machine") {
		t.Errorf("the capture did not record the actor's sort:\n%s", written)
	}
}
