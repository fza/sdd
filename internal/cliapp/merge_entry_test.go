package cliapp_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/test"
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

	t.Run("a conflict fails the command", func(t *testing.T) {
		base, ours, theirs := write("base", "base.md"), write("ours", "body-ours.md"), write("theirs", "body-theirs.md")
		_, err := runAppErr(t, dir, "merge-entry", base, ours, theirs, "7", ".sdd/graph/entry.md")
		if !errors.Is(err, handlers.ErrEntryConflict) {
			t.Errorf("a conflicted merge should fail with the conflict, got %v", err)
		}
	})

	t.Run("too few arguments are refused", func(t *testing.T) {
		if _, err := runAppErr(t, dir, "merge-entry", "base", "ours"); err == nil {
			t.Error("merge-entry without theirs should fail")
		}
	})
}
