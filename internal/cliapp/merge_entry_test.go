package cliapp_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/test"
	"github.com/urfave/cli/v3"
)

const entryAttribute = ".sdd/graph/**/*.md merge=sdd-entry"

func TestInitSetsUpTheEntryMergeDriver(t *testing.T) {
	repo := canonicalTemp(t)
	initRepo(t, repo)

	runApp(t, repo, "init", "--scope", "project", "--agents", "claude", "--language", "en", "--participant", "Ada")

	attributes := readFile(t, filepath.Join(repo, ".gitattributes"))
	if !strings.Contains(attributes, entryAttribute+"\n") {
		t.Errorf(".gitattributes should route graph entries to the driver:\n%s", attributes)
	}
	if committed := gitIn(t, repo, "ls-files", ".gitattributes"); committed == "" {
		t.Error("init should commit .gitattributes")
	}
	if driver := strings.TrimSpace(gitIn(t, repo, "config", "--local", "--get", "merge.sdd-entry.driver")); driver != git.MergeDriverCommand {
		t.Errorf("init should configure the driver command, got %q", driver)
	}

	config := readFile(t, filepath.Join(repo, ".git", "config"))
	head := gitIn(t, repo, "rev-parse", "HEAD")
	runApp(t, repo, "init")
	if again := readFile(t, filepath.Join(repo, ".gitattributes")); again != attributes {
		t.Errorf("a repeat init should leave .gitattributes alone:\n%s", again)
	}
	if again := readFile(t, filepath.Join(repo, ".git", "config")); again != config {
		t.Errorf("a repeat init should leave the git config alone:\n%s", again)
	}
	if gitIn(t, repo, "rev-parse", "HEAD") != head {
		t.Error("a repeat init should commit nothing")
	}

	gitIn(t, repo, "config", "--local", "--remove-section", "merge.sdd-entry")
	runApp(t, repo, "init")
	if driver := strings.TrimSpace(gitIn(t, repo, "config", "--local", "--get", "merge.sdd-entry.driver")); driver != git.MergeDriverCommand {
		t.Errorf("init with nothing to commit should still restore the driver command, got %q", driver)
	}
}

func TestMergeEntryCommand(t *testing.T) {
	// git runs the driver from the repository root, inside a project whose
	// config would otherwise draw warnings and a sync check.
	dir := canonicalTemp(t)
	initRepo(t, dir)
	writeFile(t, filepath.Join(dir, ".sdd", "config.yaml"), "graph_dir: .sdd/graph\nunknown_setting: true\n")
	write := func(name, fixture string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(test.MergeEntryFixture(t, fixture)), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("merges and writes the result into ours", func(t *testing.T) {
		base, ours, theirs := write("base", "base.md"), write("ours", "summary-b.md"), write("theirs", "summary-a.md")
		output, err := runAppErr(t, dir, "merge-entry", base, ours, theirs, "7", ".sdd/graph/entry.md")
		if err != nil {
			t.Fatalf("merge-entry: %v\n%s", err, output)
		}
		if output != "" {
			t.Errorf("merge-entry runs inside git's merge and should print nothing:\n%s", output)
		}
		if merged := readFile(t, ours); merged != test.MergeEntryFixture(t, "summary-a.md") {
			t.Errorf("ours should hold the merged entry:\n%s", merged)
		}
	})

	t.Run("a conflict exits 1 with markers written", func(t *testing.T) {
		base, ours, theirs := write("base", "base.md"), write("ours", "body-ours.md"), write("theirs", "body-theirs.md")
		_, err := runAppErr(t, dir, "merge-entry", base, ours, theirs, "7", ".sdd/graph/entry.md")
		if code := exitCode(err); code != 1 {
			t.Errorf("a conflicted merge should exit with the conflict code, got %d (%v)", code, err)
		}
		if !strings.Contains(readFile(t, ours), "<<<<<<<") {
			t.Error("a conflicted merge should leave markers in ours")
		}
	})

	t.Run("any other failure exits 2 and leaves ours untouched", func(t *testing.T) {
		ours, theirs := write("ours", "summary-b.md"), write("theirs", "summary-a.md")
		_, err := runAppErr(t, dir, "merge-entry", filepath.Join(dir, "missing"), ours, theirs, "7", ".sdd/graph/entry.md")
		if code := exitCode(err); code != 2 {
			t.Errorf("a failed merge should exit with the fallback code, got %d (%v)", code, err)
		}
		if readFile(t, ours) != test.MergeEntryFixture(t, "summary-b.md") {
			t.Error("a failed merge should leave ours as git handed it over")
		}
	})

	t.Run("too few arguments exit 2", func(t *testing.T) {
		_, err := runAppErr(t, dir, "merge-entry", "base", "ours")
		if code := exitCode(err); code != 2 {
			t.Errorf("merge-entry without theirs should exit with the fallback code, got %d (%v)", code, err)
		}
	})
}

func TestInitRefusesAGraphDirThePatternCannotName(t *testing.T) {
	tests := []struct {
		name     string
		graphDir func(repo string) string
	}{
		{name: "glob characters", graphDir: func(string) string { return "graph*" }},
		{name: "a parent step", graphDir: func(string) string { return "../graph" }},
		{name: "an absolute path", graphDir: func(repo string) string { return filepath.Join(repo, "graph") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := canonicalTemp(t)
			initRepo(t, repo)
			output, err := runAppErr(t, repo, "init", "--scope", "project", "--agents", "claude", "--language", "en", "--participant", "Ada", "--graph-dir", tt.graphDir(repo))
			if err == nil || !strings.Contains(err.Error(), ".gitattributes") {
				t.Errorf("init should refuse a graph directory no .gitattributes pattern can name, got %v\n%s", err, output)
			}
		})
	}
}

// exitCode is the process exit code main derives from a command's error.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit cli.ExitCoder
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return 1
}
