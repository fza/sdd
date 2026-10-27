package test

import (
	"os/exec"
	"strings"
	"testing"
)

// GitRepo initialises a repository in a temporary directory with a local
// identity and an empty first commit, and returns its path.
func GitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	Git(t, dir, "init", "-q")
	Git(t, dir, "config", "user.email", "test@example.com")
	Git(t, dir, "config", "user.name", "Test")
	Git(t, dir, "commit", "-q", "--allow-empty", "-m", "seed")
	return dir
}

// Git runs git in dir and returns its combined output, failing the test on
// a non-zero exit.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
