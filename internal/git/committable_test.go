package git_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/test"
)

func TestCommittable(t *testing.T) {
	dir := test.GitRepo(t)
	write := func(name string) string {
		path := filepath.Join(dir, name)
		mustNot(t, os.MkdirAll(filepath.Dir(path), 0o755))
		mustNot(t, os.WriteFile(path, []byte(name+"\n"), 0o644))
		return path
	}
	mustNot(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".claude/\n"), 0o644))
	removedTracked := write("removed.md")
	forcedIgnored := write(".claude/forced.md")
	test.Git(t, dir, "add", "-f", ".gitignore", removedTracked, forcedIgnored)
	test.Git(t, dir, "commit", "-q", "-m", "tracked")
	mustNot(t, os.Remove(removedTracked))

	config := write(".sdd/config.yaml")
	ignored := write(".claude/skills/sdd/SKILL.md")
	vanished := filepath.Join(dir, ".agents/skills/gone.md")
	vanishedIgnored := filepath.Join(dir, ".claude/skills/gone.md")

	cases := []struct {
		name  string
		paths []string
		want  []string
	}{
		{name: "keeps written paths", paths: []string{config}, want: []string{config}},
		{name: "drops untracked ignored paths", paths: []string{config, ignored}, want: []string{config}},
		{name: "keeps tracked paths under an ignore rule", paths: []string{forcedIgnored}, want: []string{forcedIgnored}},
		{name: "keeps a tracked path removed from disk", paths: []string{removedTracked}, want: []string{removedTracked}},
		{name: "drops an untracked path removed from disk", paths: []string{vanished, config}, want: []string{config}},
		{name: "drops an ignored path removed from disk", paths: []string{vanishedIgnored}, want: []string{}},
		{name: "nothing in, nothing out", paths: nil, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := git.CLI{Dir: dir}.Committable(t.Context(), tc.paths)
			mustNot(t, err)
			if !slices.Equal(got, tc.want) {
				t.Errorf("only paths git can stage should remain, got %v", got)
			}
		})
	}
}

func TestCommittable_CommitSucceedsOnTheResult(t *testing.T) {
	dir := test.GitRepo(t)
	mustNot(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".claude/\n"), 0o644))
	mustNot(t, os.MkdirAll(filepath.Join(dir, ".claude/skills"), 0o755))
	ignored := filepath.Join(dir, ".claude/skills/SKILL.md")
	mustNot(t, os.WriteFile(ignored, []byte("skill\n"), 0o644))
	gitignore := filepath.Join(dir, ".gitignore")
	adapter := git.CLI{Dir: dir}

	paths, err := adapter.Committable(t.Context(), []string{gitignore, ignored})
	mustNot(t, err)
	mustNot(t, adapter.Commit("sdd: refresh", paths...))

	status := test.Git(t, dir, "status", "--porcelain")
	if status != "" {
		t.Errorf("the commit should leave nothing staged or modified:\n%s", status)
	}
}

func TestUncommitted(t *testing.T) {
	dir := test.GitRepo(t)
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		mustNot(t, os.MkdirAll(filepath.Dir(path), 0o755))
		mustNot(t, os.WriteFile(path, []byte(content), 0o644))
		return path
	}
	mustNot(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".claude/\n"), 0o644))
	unchanged := write("unchanged.md", "a\n")
	modified := write("modified.md", "a\n")
	staged := write("staged.md", "a\n")
	removed := write("removed.md", "a\n")
	test.Git(t, dir, "add", ".gitignore", unchanged, modified, staged, removed)
	test.Git(t, dir, "commit", "-q", "-m", "tracked")
	write("modified.md", "b\n")
	write("staged.md", "b\n")
	test.Git(t, dir, "add", staged)
	mustNot(t, os.Remove(removed))
	untracked := write("skills/new.md", "a\n")
	ignored := write(".claude/skills/SKILL.md", "a\n")
	vanished := filepath.Join(dir, "gone.md")

	cases := []struct {
		name  string
		paths []string
		want  []string
	}{
		{name: "keeps modified, staged, removed and untracked paths", paths: []string{modified, staged, removed, untracked}, want: []string{modified, staged, removed, untracked}},
		{name: "drops an unchanged tracked path", paths: []string{unchanged, modified}, want: []string{modified}},
		{name: "drops an ignored path", paths: []string{ignored}, want: nil},
		{name: "drops a path that never existed", paths: []string{vanished}, want: nil},
		{name: "keeps a directory with a change beneath it", paths: []string{filepath.Join(dir, "skills")}, want: []string{filepath.Join(dir, "skills")}},
		{name: "nothing in, nothing out", paths: nil, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := git.CLI{Dir: dir}.Uncommitted(t.Context(), tc.paths)
			mustNot(t, err)
			if !slices.Equal(got, tc.want) {
				t.Errorf("only paths with a change git can commit should remain, got %v", got)
			}
		})
	}
}

func mustNot(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
