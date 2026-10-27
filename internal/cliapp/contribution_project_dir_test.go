package cliapp_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// In a project_dir layout the graph and its branches live in the project's
// repository, so contribution mode is set in that checkout's local config and
// judges the branch the project's checkout has checked out.
func TestContributionModeInAPointedLayout(t *testing.T) {
	work, project := pointedRepos(t)
	gitIn(t, project, "branch", "-M", "main")
	gitIn(t, project, "update-ref", "refs/remotes/origin/main", "HEAD")
	writeFile(t, filepath.Join(project, ".sdd", "config.local.yaml"), "participant: Ada\ncontribution: origin/main\n")

	before := headSubject(t, project)
	output, err := runAppErr(t, work, "new", "signal", "tactical", "A contributed gap.",
		"--confidence", "high", "--skip-preflight", "--summary", "S.", "--title", "T")
	if err == nil || !strings.Contains(err.Error()+output, "main is the default branch") {
		t.Fatalf("a capture onto the project's default branch should be refused, got %v\n%s", err, output)
	}
	if headSubject(t, project) != before {
		t.Error("a refused capture should commit nothing in the project")
	}

	gitIn(t, project, "checkout", "-q", "-b", "feat/x", "origin/main")
	captureSignal(t, work)
	if subject := headSubject(t, project); !strings.HasPrefix(subject, "sdd:") {
		t.Errorf("the capture should land on the project's feature branch, its HEAD is %q", subject)
	}
	if branch := strings.TrimSpace(gitIn(t, project, "branch", "--show-current")); branch != "feat/x" {
		t.Errorf("the project should stay on its feature branch, it is on %q", branch)
	}
}
