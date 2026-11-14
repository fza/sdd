package cliapp

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

// A connected repository's runtime looks up the files and commits a deep
// verifier cites in that repository's checkout, so a valid source passes.
func TestCloneRuntimeOptions_LookUpSourcesInTheConnectedCheckout(t *testing.T) {
	dir := t.TempDir()
	gitRun := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=Ada", "-c", "user.email=ada@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	gitRun("init", "-q", "-b", "main")
	err := os.MkdirAll(filepath.Join(dir, ".sdd", "graph"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, "cited.go"), []byte("package cited\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	gitRun("add", "cited.go")
	gitRun("commit", "-q", "-m", "add the cited file")
	head := gitRun("rev-parse", "HEAD")
	state := t.TempDir()
	locations := repos.Locations{ConfigPath: filepath.Join(state, "config.yaml"), StateRoot: filepath.Join(state, "state"), DataRoot: filepath.Join(state, "data")}

	options, err := cloneRuntimeOptions(t.Context(), "example.test/acme/platform", dir, &model.PerRepoConfig{DefaultBranch: "main"}, locations, nil, nil, nil, nil, filepath.Join(state, "cache"))
	if err != nil {
		t.Fatal(err)
	}

	if options.Checkout == nil {
		t.Fatal("the connected runtime should read cited files from its checkout")
	}
	data, err := fs.ReadFile(options.Checkout, "cited.go")
	if err != nil || string(data) != "package cited\n" {
		t.Fatalf("the checkout should hold the cited file, got %q, %v", data, err)
	}
	if options.Commits == nil {
		t.Fatal("the connected runtime should look up cited commits in its repository")
	}
	found, err := options.Commits.HasCommit(t.Context(), head)
	if err != nil || !found {
		t.Fatalf("the repository's own commit should be found, got %v, %v", found, err)
	}
}
