package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The flag is the only way the field gets set: it is never inferred from prose
// and never assumed from a name, so a consumer can trust what it reads.
func TestNewActorRecordsTheActorKind(t *testing.T) {
	project := canonicalTempDir(t)
	if err := os.MkdirAll(filepath.Join(project, ".sdd", "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, project)

	sddDir := filepath.Join(project, ".sdd")
	if err := os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("graph_dir: .sdd/graph\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sddDir, "config.local.yaml"), []byte("participant: Ada\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runSDD(t, project,
		"new", "signal", "process", "--kind", "actor",
		"--canonical", "Claude",
		"--actor-kind", "machine",
		"--confidence", "high",
		"--skip-preflight",
		"--summary", "An assistant participating in the project.",
		"Claude is an assistant working on this project; the decision behind its proposals belongs to a human participant.",
	)

	entries := graphEntries(t, filepath.Join(sddDir, "graph"))
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %v", entries)
	}
	written, err := os.ReadFile(filepath.Join(sddDir, "graph", entries[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "actor_kind: machine") {
		t.Errorf("the capture did not record the actor's sort:\n%s", written)
	}
}
