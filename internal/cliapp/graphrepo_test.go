package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The graph inside the project's own repository stays the default: one repo,
// one history, capture commits beside the work.
func TestGraphInsideTheProjectCommitsThere(t *testing.T) {
	project := canonicalTemp(t)
	initRepo(t, project)
	if err := os.MkdirAll(filepath.Join(project, ".sdd", "graph"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\n")
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")

	runApp(t, project,
		"new", "signal", "tactical",
		"The graph living inside the project's repository keeps one history for work and captures alike.",
		"--confidence", "high", "--skip-preflight",
		"--summary", "An in-repo graph keeps captures beside the work.",
		"--title", "In-repo graph",
	)

	if entries := graphEntries(t, filepath.Join(project, ".sdd", "graph")); len(entries) != 1 {
		t.Fatalf("expected one entry in the project's graph, got %v", entries)
	}
	if subject := headSubject(t, project); !strings.HasPrefix(subject, "sdd: ") {
		t.Errorf("the capture commit is missing from the project's history: %q", subject)
	}
}

func twoRepos(t *testing.T) (project, sidecar string) {
	t.Helper()
	root := canonicalTemp(t)
	project = filepath.Join(root, "project")
	sidecar = filepath.Join(root, "sidecar")
	for _, dir := range []string{project, filepath.Join(sidecar, "graph")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	initRepo(t, project)
	initRepo(t, sidecar)
	return project, sidecar
}

func headSubject(t *testing.T, repo string) string {
	t.Helper()
	return strings.TrimSpace(gitIn(t, repo, "log", "-1", "--format=%s"))
}

// The in-repo default keeps deriving from the one repository it has.
func TestInitDerivesIdentityFromTheProjectWhenTheGraphIsInside(t *testing.T) {
	project := canonicalTemp(t)
	initRepo(t, project)
	gitIn(t, project, "remote", "add", "origin", "git@gitlab.com:acme/project.git")

	runApp(t, project, "init",
		"--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude",
	)

	if written := readFile(t, filepath.Join(project, ".sdd", "config.yaml")); !strings.Contains(written, "repo_id: gitlab.com/acme/project") {
		t.Errorf("repo_id must name the project's own repository:\n%s", written)
	}
}

// init refuses the graph_dir every later command would refuse.
func TestInitRefusesAGraphDirOutsideAnyRepository(t *testing.T) {
	root := canonicalTemp(t)
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, project)

	_, err := runAppErr(t, project, "init", "--graph-dir", "../norepo/graph", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")

	if err == nil || !strings.Contains(err.Error(), "outside any repository") {
		t.Fatalf("a graph_dir outside any repository should be refused at init, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(project, ".sdd")); !os.IsNotExist(statErr) {
		t.Errorf("a refused init should write nothing, stat err = %v", statErr)
	}
}
