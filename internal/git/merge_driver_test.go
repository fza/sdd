package git_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/test"
)

const driverKey = "merge.sdd-entry.driver"

// driverConfig returns the repository-local driver command, or "" when unset.
func driverConfig(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "config", "--local", "--get", driverKey).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func TestEnsureMergeDriver(t *testing.T) {
	t.Run("writes the driver section into a repository lacking it", func(t *testing.T) {
		dir := test.GitRepo(t)
		if err := (git.CLI{Dir: dir}).EnsureMergeDriver(context.Background()); err != nil {
			t.Fatalf("EnsureMergeDriver: %v", err)
		}
		if got := driverConfig(t, dir); got != git.MergeDriverCommand {
			t.Errorf("the driver command should be configured, got %q", got)
		}
	})

	t.Run("a second run changes nothing", func(t *testing.T) {
		dir := test.GitRepo(t)
		repo := git.CLI{Dir: dir}
		if err := repo.EnsureMergeDriver(context.Background()); err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(dir, ".git", "config")
		before, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.EnsureMergeDriver(context.Background()); err != nil {
			t.Fatalf("EnsureMergeDriver, again: %v", err)
		}
		after, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Errorf("config should stay unchanged:\n%s\nwas\n%s", after, before)
		}
	})

	t.Run("a driver configured otherwise is kept", func(t *testing.T) {
		dir := test.GitRepo(t)
		test.Git(t, dir, "config", driverKey, "custom %O %A %B")
		if err := (git.CLI{Dir: dir}).EnsureMergeDriver(context.Background()); err != nil {
			t.Fatalf("EnsureMergeDriver: %v", err)
		}
		if got := driverConfig(t, dir); got != "custom %O %A %B" {
			t.Errorf("a custom driver should be left alone, got %q", got)
		}
	})

	for _, earlier := range []string{
		"sdd merge-entry %O %A %B %L %P",
		"sdd merge-entry %O %A %B %L %P; s=$?; [ $s -le 1 ] && exit $s; git merge-file -L ours -L base -L theirs --marker-size=%L %A %O %B",
	} {
		t.Run("a command an earlier sdd wrote is replaced: "+earlier, func(t *testing.T) {
			dir := test.GitRepo(t)
			test.Git(t, dir, "config", driverKey, earlier)
			if err := (git.CLI{Dir: dir}).EnsureMergeDriver(context.Background()); err != nil {
				t.Fatalf("EnsureMergeDriver: %v", err)
			}
			if got := driverConfig(t, dir); got != git.MergeDriverCommand {
				t.Errorf("an earlier sdd driver command should be upgraded, got %q", got)
			}
		})
	}

	t.Run("a config that cannot be written is reported", func(t *testing.T) {
		dir := test.GitRepo(t)
		if err := os.WriteFile(filepath.Join(dir, ".git", "config.lock"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := (git.CLI{Dir: dir}).EnsureMergeDriver(context.Background()); err == nil {
			t.Error("a driver setup that cannot write the config should fail")
		}
	})

	t.Run("a directory outside any repository is left alone", func(t *testing.T) {
		dir := t.TempDir()
		if err := (git.CLI{Dir: dir}).EnsureMergeDriver(context.Background()); err != nil {
			t.Fatalf("EnsureMergeDriver outside a repository: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
			t.Errorf("no repository should be created, stat: %v", err)
		}
	})

	t.Run("a failure to locate the repository is reported", func(t *testing.T) {
		dir := test.GitRepo(t)
		t.Setenv("PATH", t.TempDir())
		if err := (git.CLI{Dir: dir}).EnsureMergeDriver(context.Background()); err == nil {
			t.Error("a git that cannot run should fail the setup, not read as no repository")
		}
	})
}

func TestRemovalCommitterSetsUpTheMergeDriver(t *testing.T) {
	dir := test.GitRepo(t)
	if err := (git.RemovalCommitter{Dir: dir}).EnsureMergeDriver(context.Background()); err != nil {
		t.Fatalf("EnsureMergeDriver: %v", err)
	}
	if got := driverConfig(t, dir); got != git.MergeDriverCommand {
		t.Errorf("the removal committer should set up the driver of its repository, got %q", got)
	}
}

// fallbackRepo holds one entry whose summary both sides changed, the
// .gitattributes line and the driver section, ready for `git merge other`.
func fallbackRepo(t *testing.T) string {
	t.Helper()
	dir := test.GitRepo(t)
	write := func(fixture string) {
		err := os.WriteFile(filepath.Join(dir, "entry.md"), []byte(test.MergeEntryFixture(t, fixture)), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.md merge=sdd-entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (git.CLI{Dir: dir}).EnsureMergeDriver(context.Background()); err != nil {
		t.Fatal(err)
	}
	write("base.md")
	test.Git(t, dir, "add", ".")
	test.Git(t, dir, "commit", "-q", "-m", "base")
	base := strings.TrimSpace(test.Git(t, dir, "rev-parse", "--abbrev-ref", "HEAD"))
	test.Git(t, dir, "checkout", "-q", "-b", "other")
	write("summary-a.md")
	test.Git(t, dir, "commit", "-q", "-am", "theirs")
	test.Git(t, dir, "checkout", "-q", base)
	write("summary-b.md")
	test.Git(t, dir, "commit", "-q", "-am", "ours")
	return dir
}

// An sdd that cannot run merge-entry leaves git's text merge, conflict
// markers included, never a file holding one side only.
func TestDriverFallsBackToTextMerge(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		sdd  string
	}{
		{name: "an sdd without merge-entry", sdd: "#!/bin/sh\necho \"No help topic for '$1'\" >&2\nexit 3\n"},
		{name: "an sdd failing before the command runs", sdd: "#!/bin/sh\necho 'invalid value for flag -timing' >&2\nexit 1\n"},
		{name: "no sdd on PATH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := fallbackRepo(t)
			bin := t.TempDir()
			if err := os.Symlink(gitPath, filepath.Join(bin, "git")); err != nil {
				t.Fatal(err)
			}
			if tt.sdd != "" {
				if err := os.WriteFile(filepath.Join(bin, "sdd"), []byte(tt.sdd), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("git", "-C", dir, "merge", "-q", "--no-edit", "other")
			cmd.Env = append(os.Environ(), "PATH="+bin+":/bin")
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("both summaries changed should conflict under the fallback text merge:\n%s", out)
			}
			merged, err := os.ReadFile(filepath.Join(dir, "entry.md"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"<<<<<<< ours", "A summary, sorting first.", "B summary, sorting second.", ">>>>>>> theirs"} {
				if !strings.Contains(string(merged), want) {
					t.Errorf("the fallback should leave both sides between markers, missing %q:\n%s\n%s", want, merged, out)
				}
			}
		})
	}
}

// pathWithSDD returns a PATH holding git and an sdd running script.
func pathWithSDD(t *testing.T, script string) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(gitPath, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "sdd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin + ":/bin"
}

// What merge-entry wrote stands, merged or conflicted: the text merge never
// runs over it.
func TestDriverKeepsWhatMergeEntryWrote(t *testing.T) {
	tests := []struct {
		name         string
		written      string
		exit         int
		wantConflict bool
	}{
		{name: "merged", written: "merged by merge-entry\n"},
		{name: "conflicted", written: "<<<<<<< ours\nwritten by merge-entry\n>>>>>>> theirs\n", exit: git.MergeEntryConflictExit, wantConflict: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := fallbackRepo(t)
			script := fmt.Sprintf("#!/bin/sh\nprintf '%s' > \"$3\"\nexit %d\n", strings.ReplaceAll(tt.written, "\n", "\\n"), tt.exit)
			cmd := exec.Command("git", "-C", dir, "merge", "-q", "--no-edit", "other")
			cmd.Env = append(os.Environ(), "PATH="+pathWithSDD(t, script))
			out, err := cmd.CombinedOutput()
			if (err != nil) != tt.wantConflict {
				t.Fatalf("the merge should report what merge-entry reported, err %v:\n%s", err, out)
			}
			merged, err := os.ReadFile(filepath.Join(dir, "entry.md"))
			if err != nil {
				t.Fatal(err)
			}
			if string(merged) != tt.written {
				t.Errorf("the file merge-entry wrote should stand as written:\n%s\n%s", merged, out)
			}
		})
	}
}

func TestMergePathsSetUpTheMergeDriver(t *testing.T) {
	t.Run("merge", func(t *testing.T) {
		dir := test.GitRepo(t)
		test.Git(t, dir, "branch", "other")
		if err := (git.CLI{Dir: dir}).Merge(context.Background(), "other", "sdd: merge other"); err != nil {
			t.Fatalf("Merge: %v", err)
		}
		if got := driverConfig(t, dir); got != git.MergeDriverCommand {
			t.Errorf("a merge should set up the merge driver, got %q", got)
		}
	})

	t.Run("merge pull", func(t *testing.T) {
		upstream := test.GitRepo(t)
		clone := filepath.Join(t.TempDir(), "clone")
		if out, err := exec.Command("git", "clone", "-q", upstream, clone).CombinedOutput(); err != nil {
			t.Fatalf("git clone: %v\n%s", err, out)
		}
		if _, err := (git.CLI{Dir: clone}).MergePull(context.Background()); err != nil {
			t.Fatalf("MergePull: %v", err)
		}
		if got := driverConfig(t, clone); got != git.MergeDriverCommand {
			t.Errorf("a merge pull should set up the merge driver, got %q", got)
		}
	})
}

func TestMergeFile(t *testing.T) {
	tests := []struct {
		name           string
		ours, theirs   string
		wantConflicted bool
		want           string
	}{
		{name: "separate lines merge cleanly", ours: "A\nb\nc\n", theirs: "a\nb\nC\n", want: "A\nb\nC\n"},
		{name: "the same line changed twice conflicts", ours: "a\nB1\nc\n", theirs: "a\nB2\nc\n", wantConflicted: true, want: "<<<<<<< ours"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, content string) string {
				path := filepath.Join(dir, name)
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				return path
			}
			current := write("ours", tt.ours)
			conflicted, err := git.CLI{}.MergeFile(context.Background(), current, write("base", "a\nb\nc\n"), write("theirs", tt.theirs), 0)
			if err != nil {
				t.Fatalf("MergeFile: %v", err)
			}
			if conflicted != tt.wantConflicted {
				t.Errorf("conflicted = %v, want %v", conflicted, tt.wantConflicted)
			}
			merged, err := os.ReadFile(current)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(merged), tt.want) {
				t.Errorf("merged file should hold %q:\n%s", tt.want, merged)
			}
		})
	}
}

// Without the driver section a clone keeps git's text merge: the
// .gitattributes line alone neither breaks a merge nor resolves a summary.
func TestEntriesTextMergeWithoutTheDriverSection(t *testing.T) {
	dir := test.GitRepo(t)
	write := func(fixture string) {
		err := os.WriteFile(filepath.Join(dir, "entry.md"), []byte(test.MergeEntryFixture(t, fixture)), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.md merge=sdd-entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write("base.md")
	test.Git(t, dir, "add", ".")
	test.Git(t, dir, "commit", "-q", "-m", "base")
	base := strings.TrimSpace(test.Git(t, dir, "rev-parse", "--abbrev-ref", "HEAD"))
	test.Git(t, dir, "checkout", "-q", "-b", "other")
	write("summary-a.md")
	test.Git(t, dir, "commit", "-q", "-am", "theirs")
	test.Git(t, dir, "checkout", "-q", base)
	write("summary-b.md")
	test.Git(t, dir, "commit", "-q", "-am", "ours")

	out, err := exec.Command("git", "-C", dir, "merge", "-q", "--no-edit", "other").CombinedOutput()
	if err == nil {
		t.Fatalf("both summaries changed should conflict under git's text merge:\n%s", out)
	}
	merged, err := os.ReadFile(filepath.Join(dir, "entry.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(merged), "<<<<<<<") {
		t.Errorf("git's text merge should leave conflict markers:\n%s\n%s", merged, out)
	}
	if driverConfig(t, dir) != "" {
		t.Errorf("a plain git merge should not set up the driver")
	}
}
