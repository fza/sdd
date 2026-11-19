package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const besideEntryID = "20260914-020000-s-tac-cli"

func refusedCapture(t *testing.T, dir string) (string, error) {
	t.Helper()
	return runAppErr(t, dir,
		"new", "signal", "tactical", "A capture whose commit runs in the working directory.",
		"--confidence", "high", "--skip-preflight", "--summary", "A capture beside its repository.", "--title", "Capture beside",
	)
}

// A graph_dir leading into another repository is read and written there under
// the code repository's settings. The commit runs in the working directory,
// whose repository does not hold the entry.
func TestGraphDirIntoAnotherRepository(t *testing.T) {
	cases := []struct {
		name     string
		graphDir func(sidecar string) string
	}{
		{name: "relative path", graphDir: func(string) string { return "../sidecar/graph" }},
		{name: "absolute path", graphDir: func(sidecar string) string { return filepath.Join(sidecar, "graph") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project, sidecar := twoRepos(t)
			writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: "+tc.graphDir(sidecar)+"\nrepo_id: example.test/project\ndefault_branch: main\nverify:\n  mode: off\n")
			writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")
			writeFile(t, filepath.Join(sidecar, ".sdd", "config.yaml"), "graph_dir: elsewhere\nrepo_id: example.test/sidecar\ndefault_branch: main\n")
			writeFile(t, filepath.Join(sidecar, ".sdd", "config.local.yaml"), "participant: Bo\n")
			writeFile(t, filepath.Join(sidecar, "graph", "2026", "09", "14-020000-s-tac-cli.md"), "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\nparticipants: [Ada]\nsummary: An entry in the other repository.\n---\n\nRead across repositories.\n")

			shown, err := runAppErr(t, project, "show", besideEntryID)
			if err != nil || !strings.Contains(shown, "Read across repositories.") {
				t.Fatalf("the graph in the other repository should be read: %v\n%s", err, shown)
			}

			out, err := refusedCapture(t, project)

			if err == nil || !strings.Contains(err.Error()+out, "outside repository") {
				t.Fatalf("the commit should fail as git refuses a path outside the working directory's repository, got %v:\n%s", err, out)
			}
			entries := graphEntries(t, filepath.Join(sidecar, "graph"))
			if len(entries) != 2 {
				t.Fatalf("the capture should be written into the other repository's graph, got %v", entries)
			}
			if _, statErr := os.Stat(filepath.Join(sidecar, "elsewhere")); !os.IsNotExist(statErr) {
				t.Errorf("the other repository's own graph_dir should be ignored, stat err = %v", statErr)
			}
			for _, entry := range entries {
				if strings.Contains(readFile(t, filepath.Join(sidecar, "graph", entry)), "Bo") {
					t.Errorf("the other repository's own participant should be ignored in %s", entry)
				}
			}
			for _, repo := range []string{project, sidecar} {
				if subject := headSubject(t, repo); subject != "seed" {
					t.Errorf("no repository should gain a commit, HEAD of %s is %q", repo, subject)
				}
			}
		})
	}
}

func TestGraphDirOutsideAnyRepository(t *testing.T) {
	project, _ := twoRepos(t)
	outside := filepath.Join(canonicalTemp(t), "graph")
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: "+outside+"\nrepo_id: example.test/project\ndefault_branch: main\nverify:\n  mode: off\n")
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")

	out, err := refusedCapture(t, project)

	if err == nil || !strings.Contains(err.Error()+out, "outside repository") {
		t.Fatalf("the commit should fail as git refuses a path outside the working directory's repository, got %v:\n%s", err, out)
	}
	if entries := graphEntries(t, outside); len(entries) != 1 {
		t.Errorf("the capture should be written into the graph folder, got %v", entries)
	}
}

// A repository nested in a project, holding no .sdd/ of its own, works on the
// enclosing project's graph, and its commit runs in the nested repository.
func TestCaptureFromARepositoryNestedInAProject(t *testing.T) {
	project := canonicalTemp(t)
	initRepo(t, project)
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nrepo_id: example.test/project\ndefault_branch: main\nverify:\n  mode: off\n")
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")
	nested := filepath.Join(project, "nested")
	err := os.MkdirAll(nested, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	initRepo(t, nested)

	out, err := refusedCapture(t, nested)

	if err == nil || !strings.Contains(err.Error()+out, "outside repository") {
		t.Fatalf("the commit should fail as git refuses a path outside the nested repository, got %v:\n%s", err, out)
	}
	if entries := graphEntries(t, filepath.Join(project, ".sdd", "graph")); len(entries) != 1 {
		t.Errorf("the capture should be written into the enclosing project's graph, got %v", entries)
	}
	for _, repo := range []string{project, nested} {
		if subject := headSubject(t, repo); subject != "seed" {
			t.Errorf("no repository should gain a commit, HEAD of %s is %q", repo, subject)
		}
	}
}

// init sets up a graph_dir leading out of the repository as it sets up any
// other.
func TestInitAcceptsAGraphDirOutsideTheRepository(t *testing.T) {
	cases := []struct {
		name     string
		graphDir string
	}{
		{name: "into another repository", graphDir: "../sidecar/graph"},
		{name: "outside any repository", graphDir: "../norepo/graph"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project, _ := twoRepos(t)

			runApp(t, project, "init", "--graph-dir", tc.graphDir, "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")

			if written := readFile(t, filepath.Join(project, ".sdd", "config.yaml")); !strings.Contains(written, "graph_dir: "+tc.graphDir+"\n") {
				t.Errorf("the config should record the graph_dir as given:\n%s", written)
			}
			info, err := os.Stat(filepath.Join(project, filepath.FromSlash(tc.graphDir)))
			if err != nil || !info.IsDir() {
				t.Errorf("init should create the graph folder: %v", err)
			}
		})
	}
}
