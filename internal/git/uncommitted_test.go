package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/git"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "user.name", "Test")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	t.Chdir(dir)
	return dir
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeIn(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUncommitted(t *testing.T) {
	dir := gitRepo(t)
	writeIn(t, dir, ".gitignore", ".claude/\n")
	unchanged := writeIn(t, dir, "unchanged.md", "a\n")
	modified := writeIn(t, dir, "modified.md", "a\n")
	staged := writeIn(t, dir, "staged.md", "a\n")
	removed := writeIn(t, dir, "removed.md", "a\n")
	gitIn(t, dir, "add", ".gitignore", unchanged, modified, staged, removed)
	gitIn(t, dir, "commit", "-q", "-m", "tracked")
	writeIn(t, dir, "modified.md", "b\n")
	writeIn(t, dir, "staged.md", "b\n")
	gitIn(t, dir, "add", staged)
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}
	untracked := writeIn(t, dir, "skills/new.md", "a\n")
	ignored := writeIn(t, dir, ".claude/skills/SKILL.md", "a\n")
	vanished := filepath.Join(dir, "gone.md")
	notYetCreated := filepath.Join(dir, "later", "file.md")

	cases := []struct {
		name  string
		paths []string
		want  []string
	}{
		{name: "keeps modified, staged, removed and untracked paths", paths: []string{modified, staged, removed, untracked}, want: []string{modified, staged, removed, untracked}},
		{name: "drops an unchanged tracked path", paths: []string{unchanged, modified}, want: []string{modified}},
		{name: "drops an ignored path", paths: []string{ignored}, want: nil},
		{name: "drops a path that never existed", paths: []string{vanished, notYetCreated}, want: nil},
		{name: "keeps a directory with a change beneath it", paths: []string{filepath.Join(dir, "skills")}, want: []string{filepath.Join(dir, "skills")}},
		{name: "nothing in, nothing out", paths: nil, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := git.CLI{}.Uncommitted(t.Context(), tc.paths)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("only paths with a change git can commit should remain, got %v", got)
			}
		})
	}
}

func TestIgnored(t *testing.T) {
	dir := gitRepo(t)
	writeIn(t, dir, ".gitignore", "tmp/\n")
	tracked := writeIn(t, dir, "tmp/tracked", "a\n")
	gitIn(t, dir, "add", ".gitignore")
	gitIn(t, dir, "add", "-f", tracked)
	gitIn(t, dir, "commit", "-q", "-m", "tracked")

	cases := []struct {
		name string
		path string
		want bool
	}{
		{name: "an ignored untracked file", path: writeIn(t, dir, "tmp/record", "a\n"), want: true},
		{name: "an ignored file not yet written", path: filepath.Join(dir, "tmp", "later"), want: true},
		{name: "a file no rule matches", path: writeIn(t, dir, "record", "a\n"), want: false},
		{name: "a tracked file a rule matches", path: tracked, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := git.CLI{}.Ignored(t.Context(), tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("only an untracked path an ignore rule matches should count as ignored, got %v", got)
			}
		})
	}
}
