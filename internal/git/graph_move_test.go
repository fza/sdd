package git_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/git"
)

// A git without its subtree command refuses the move up front, naming the
// cause; one with it passes.
func TestRequireSubtree(t *testing.T) {
	err := git.CLI{}.RequireSubtree(t.Context())
	if err != nil {
		t.Skipf("this machine's git lacks subtree: %v", err)
	}

	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	fake := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = subtree ]; then echo \"git: 'subtree' is not a git command. See 'git --help'.\" >&2; exit 1; fi\nexec '" + real + "' \"$@\"\n"
	err = os.WriteFile(filepath.Join(fake, "git"), []byte(script), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))
	err = git.CLI{}.RequireSubtree(t.Context())
	if !errors.Is(err, git.ErrNoSubtree) {
		t.Errorf("a git without subtree should refuse with ErrNoSubtree, got %v", err)
	}
}
