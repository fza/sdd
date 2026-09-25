package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A graph can live in a checkout of its own beside the project, so the project's
// history carries only the work. The capture then runs where the work is and has
// to commit where the graph is.
func TestSidecarGraphCommitsLandInTheGraphRepository(t *testing.T) {
	root := canonicalTempDir(t)
	project := filepath.Join(root, "project")
	sidecar := filepath.Join(root, "sidecar")
	graphDir := filepath.Join(sidecar, "graph")

	for _, dir := range []string{project, graphDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	initRepo(t, project)
	initRepo(t, sidecar)

	sddDir := filepath.Join(project, ".sdd")
	if err := os.MkdirAll(sddDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sddDir, "config.yaml"), []byte("graph_dir: "+graphDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sddDir, "config.local.yaml"), []byte("participant: Ada\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runSDD(t, project,
		"new", "signal", "tactical",
		"The project repository carries only the work, while signals and decisions live in a checkout beside it.",
		"--confidence", "high",
		"--skip-preflight",
		"--summary", "A sidecar graph keeps the project history free of capture commits.",
	)

	entries := graphEntries(t, graphDir)
	if len(entries) != 1 {
		t.Fatalf("expected one entry in the sidecar graph, got %v", entries)
	}

	if files := headFiles(t, sidecar); len(files) == 0 || !strings.HasPrefix(files[0], "graph/") {
		t.Errorf("the sidecar's HEAD does not carry the entry; files = %v", files)
	}
	if subject := headSubject(t, project); subject != "seed" {
		t.Errorf("the project repository gained a capture commit: %q", subject)
	}
	if subject := headSubject(t, sidecar); !strings.HasPrefix(subject, "sdd: ") {
		t.Errorf("the sidecar's HEAD is not the capture commit: %q", subject)
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	gitInDir(t, dir, "init", "-q")
	gitInDir(t, dir, "config", "user.email", "test@example.com")
	gitInDir(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInDir(t, dir, "add", "seed.txt")
	gitInDir(t, dir, "commit", "-q", "-m", "seed")
}

func gitInDir(t *testing.T, dir string, args ...string) string {
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
	return strings.TrimSpace(gitInDir(t, repo, "log", "-1", "--format=%s"))
}

func headFiles(t *testing.T, repo string) []string {
	t.Helper()
	return strings.Fields(gitInDir(t, repo, "show", "--name-only", "--pretty=format:", "HEAD"))
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

// The engine write path commits the graph inside the checkout it serves, so a
// graph kept elsewhere would be written to disk and committed nowhere. Starting
// the server has to fail rather than lose commits.
func TestServeRefusesASidecarGraph(t *testing.T) {
	root := canonicalTempDir(t)
	project := filepath.Join(root, "project")
	sidecar := filepath.Join(root, "sidecar")
	graphDir := filepath.Join(sidecar, "graph")

	for _, dir := range []string{project, graphDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	initRepo(t, project)
	initRepo(t, sidecar)

	if err := requireGraphInsideCheckout(graphDir, project); err == nil {
		t.Fatal("serving a graph from another repository must fail")
	} else if !strings.Contains(err.Error(), "separate from this checkout") {
		t.Errorf("error should name the arrangement, got %v", err)
	}

	inside := filepath.Join(project, ".sdd", "graph")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := requireGraphInsideCheckout(inside, project); err != nil {
		t.Errorf("a graph inside the served checkout must be accepted: %v", err)
	}
}
