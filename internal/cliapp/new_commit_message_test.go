package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestNewCommitNamesTheEntryInSubjectAndBody(t *testing.T) {
	project := canonicalTemp(t)
	initRepo(t, project)
	graphDir := filepath.Join(project, ".sdd", "graph")
	if err := os.MkdirAll(graphDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nverify:\n  mode: off\n")
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")

	runApp(t, project,
		"new", "signal", "tactical",
		"A capture commit that names its entry can be found in the history by the entry's ID.",
		"--confidence", "high", "--skip-preflight",
		"--summary", "The capture commit names its entry.",
		"--title", "Entry ID in the commit",
	)

	entries := graphEntries(t, graphDir)
	if len(entries) != 1 {
		t.Fatalf("expected one entry in the graph, got %v", entries)
	}
	id, err := model.RelPathToID(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	want := "sdd: signal tactical Entry ID in the commit\n\nEntry: " + id
	if message := strings.TrimSpace(gitIn(t, project, "log", "-1", "--format=%B")); message != want {
		t.Errorf("the capture commit should carry the title in its subject and the entry ID in its body, got %q", message)
	}
}
