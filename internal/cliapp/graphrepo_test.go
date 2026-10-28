package cliapp_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/cliapp"
)

// A graph can live in a checkout of its own beside the project, so the
// project's history carries only the work. The capture then runs where the work
// is and has to commit where the graph is.
func TestGraphInItsOwnRepositoryCommitsThere(t *testing.T) {
	project, sidecar := twoRepos(t)
	graphDir := filepath.Join(sidecar, "graph")
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: "+graphDir+"\n")
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")

	runApp(t, project,
		"new", "signal", "tactical",
		"The project repository carries only the work, while signals and decisions live in a checkout beside it.",
		"--confidence", "high", "--skip-preflight",
		"--summary", "A graph beside the project keeps the project history free of capture commits.",
	)

	if entries := graphEntries(t, graphDir); len(entries) != 1 {
		t.Fatalf("expected one entry in the graph repository, got %v", entries)
	}
	if subject := headSubject(t, sidecar); !strings.HasPrefix(subject, "sdd: ") {
		t.Errorf("the graph repository's HEAD is not the capture commit: %q", subject)
	}
	if subject := headSubject(t, project); subject != "seed" {
		t.Errorf("the project repository gained a capture commit: %q", subject)
	}
}

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

func canonicalTemp(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func runApp(t *testing.T, dir string, args ...string) {
	t.Helper()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg-config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "xdg-cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "xdg-state"))
	var stdout, stderr bytes.Buffer
	command := cliapp.New(cliapp.Options{Version: "test", Reader: strings.NewReader(""), Writer: &stdout, ErrWriter: &stderr})
	if err := command.Run(t.Context(), append([]string{"sdd"}, args...)); err != nil {
		t.Fatalf("sdd %s: %v\n%s%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
}

func writeFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "user.name", "Test")
	writeFile(t, filepath.Join(dir, "seed.txt"), "seed\n")
	gitIn(t, dir, "add", "seed.txt")
	gitIn(t, dir, "commit", "-q", "-m", "seed")
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

func headSubject(t *testing.T, repo string) string {
	t.Helper()
	return strings.TrimSpace(gitIn(t, repo, "log", "-1", "--format=%s"))
}

func graphEntries(t *testing.T, graphDir string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(graphDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			rel, _ := filepath.Rel(graphDir, path)
			found = append(found, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", graphDir, err)
	}
	return found
}

// repo_id and default_branch describe the graph, so they come from the
// repository the graph lives in: other graphs reference entries under that
// identity, and the project's own remote names the code rather than the graph.
func TestInitDerivesIdentityFromTheGraphRepository(t *testing.T) {
	project, sidecar := twoRepos(t)
	gitIn(t, project, "remote", "add", "origin", "git@gitlab.com:acme/project.git")
	gitIn(t, sidecar, "remote", "add", "origin", "git@gitlab.com:acme/project-sdd.git")
	gitIn(t, sidecar, "checkout", "-q", "-b", "graph-main")

	runApp(t, project, "init",
		"--graph-dir", filepath.Join(sidecar, "graph"),
		"--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude",
	)

	written := readFile(t, filepath.Join(project, ".sdd", "config.yaml"))
	if !strings.Contains(written, "repo_id: gitlab.com/acme/project-sdd") {
		t.Errorf("repo_id must name the graph's repository:\n%s", written)
	}
	if strings.Contains(written, "repo_id: gitlab.com/acme/project\n") {
		t.Errorf("repo_id must not name the project's repository:\n%s", written)
	}
	if !strings.Contains(written, "default_branch: graph-main") {
		t.Errorf("default_branch must be the graph repository's branch:\n%s", written)
	}
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

// The engine write path commits the graph inside the checkout it serves, so a
// graph kept elsewhere would be written to disk and committed nowhere.
func TestServeRefusesAGraphInAnotherRepository(t *testing.T) {
	project, sidecar := twoRepos(t)
	writeFile(t, filepath.Join(project, ".sdd", "config.yaml"), "graph_dir: "+filepath.Join(sidecar, "graph")+"\nrepo_id: example.test/project\ndefault_branch: main\n")
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\n")

	t.Chdir(project)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(project, "xdg-config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(project, "xdg-cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(project, "xdg-state"))
	var stdout, stderr bytes.Buffer
	command := cliapp.New(cliapp.Options{Version: "test", Reader: strings.NewReader(""), Writer: &stdout, ErrWriter: &stderr})
	err := command.Run(t.Context(), []string{"sdd", "serve", "--transport", "http", "--addr", "127.0.0.1:0"})
	if err == nil {
		t.Fatal("serving a graph from another repository must fail")
	}
	if !strings.Contains(err.Error(), "separate from this checkout") {
		t.Errorf("error should name the arrangement, got %v", err)
	}
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
