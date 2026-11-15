package cliapp_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/cliapp"
)

// initRepo returns a git repository with one seed commit, entered as the
// working directory, with the XDG homes outside it.
func initRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	homes := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homes, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homes, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(homes, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homes, "state"))
	gitIn(t, repo, "init", "-q")
	gitIn(t, repo, "config", "user.email", "test@example.com")
	gitIn(t, repo, "config", "user.name", "Test")
	gitIn(t, repo, "config", "commit.gpgsign", "false")
	writeRepoFile(t, filepath.Join(repo, "seed.txt"), "seed\n")
	gitIn(t, repo, "add", "seed.txt")
	gitIn(t, repo, "commit", "-q", "-m", "seed")
	t.Chdir(repo)
	return repo
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

func writeRepoFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runInit(t *testing.T, args ...string) error {
	t.Helper()
	var stdout, stderr bytes.Buffer
	command := cliapp.New(cliapp.Options{Version: "test", Reader: strings.NewReader(""), Writer: &stdout, ErrWriter: &stderr})
	return command.Run(t.Context(), append([]string{"sdd", "init"}, args...))
}

func mustInit(t *testing.T, args ...string) {
	t.Helper()
	if err := runInit(t, args...); err != nil {
		t.Fatalf("sdd init %s: %v", strings.Join(args, " "), err)
	}
}

var firstInit = []string{"--scope", "project", "--agents", "claude", "--language", "en", "--participant", "Ada"}

// refuseCommits points repo at a commit-msg hook refusing every commit, and
// returns the call that lifts it.
func refuseCommits(t *testing.T, repo string) (allow func()) {
	t.Helper()
	hooks := t.TempDir()
	hook := filepath.Join(hooks, "commit-msg")
	writeRepoFile(t, hook, "#!/bin/sh\necho 'commit refused' >&2\nexit 1\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "config", "core.hooksPath", hooks)
	return func() { gitIn(t, repo, "config", "--unset", "core.hooksPath") }
}

func uncommittedRecord(repo string) string {
	return filepath.Join(repo, ".sdd", "tmp", "init-uncommitted")
}

func headCommit(t *testing.T, repo string) string {
	t.Helper()
	return strings.TrimSpace(gitIn(t, repo, "rev-parse", "HEAD"))
}

func TestInitCompletesTheCommitAnEarlierRunLeftUndone(t *testing.T) {
	repo := initRepo(t)
	allow := refuseCommits(t, repo)

	err := runInit(t, firstInit...)
	if err == nil || !strings.Contains(err.Error(), "the next sdd init retries") {
		t.Fatalf("init should fail naming the retry when its commit is refused, got %v", err)
	}
	if _, statErr := os.Stat(uncommittedRecord(repo)); statErr != nil {
		t.Fatalf("a refused commit should leave a record of the paths it held: %v", statErr)
	}
	if err := runInit(t); err == nil {
		t.Fatal("a re-run whose commit is refused again should fail")
	}
	if _, statErr := os.Stat(uncommittedRecord(repo)); statErr != nil {
		t.Fatalf("a re-run refused again should keep the record: %v", statErr)
	}

	allow()
	mustInit(t)

	if status := gitIn(t, repo, "status", "--porcelain"); status != "" {
		t.Errorf("a re-run should commit what the refused runs left uncommitted:\n%s", status)
	}
	if subject := gitIn(t, repo, "log", "-1", "--format=%s"); !strings.HasPrefix(subject, "sdd: ") {
		t.Error("the completed commit should be init's own")
	}
	if _, statErr := os.Stat(uncommittedRecord(repo)); !os.IsNotExist(statErr) {
		t.Errorf("a landed commit should remove the record: %v", statErr)
	}
}

func TestInitLeavesTheUsersOwnEditsOutOfARetriedCommit(t *testing.T) {
	repo := initRepo(t)
	mustInit(t, firstInit...)
	gitignore := filepath.Join(repo, ".gitignore")
	content, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, gitignore, string(content)+"/scratch\n")
	skill := filepath.Join(repo, ".claude", "skills", "sdd", "SKILL.md")
	gitIn(t, repo, "rm", "-q", skill)
	gitIn(t, repo, "commit", "-q", "-m", "chore: drop the skill")
	allow := refuseCommits(t, repo)
	if err := runInit(t); err == nil {
		t.Fatal("init should fail when its commit is refused")
	}
	allow()

	mustInit(t)

	if status := gitIn(t, repo, "status", "--porcelain"); status != " M .gitignore\n" {
		t.Errorf("the retried commit should take the restored skill and leave the user's own edit uncommitted:\n%s", status)
	}
	if _, statErr := os.Stat(uncommittedRecord(repo)); !os.IsNotExist(statErr) {
		t.Errorf("a landed commit should remove the record: %v", statErr)
	}
}

func TestInitDropsRecordedPathsCommittedMeanwhile(t *testing.T) {
	repo := initRepo(t)
	allow := refuseCommits(t, repo)
	if err := runInit(t, firstInit...); err == nil {
		t.Fatal("init should fail when its commit is refused")
	}
	allow()
	gitIn(t, repo, "add", "--all")
	gitIn(t, repo, "commit", "-q", "-m", "chore: commit the setup by hand")
	head := headCommit(t, repo)

	mustInit(t)

	if headCommit(t, repo) != head {
		t.Error("a re-run with nothing left uncommitted should commit nothing")
	}
	if _, statErr := os.Stat(uncommittedRecord(repo)); !os.IsNotExist(statErr) {
		t.Errorf("a record with nothing left to commit should be removed: %v", statErr)
	}
}

// A clone shipping a record: init refuses it before writing anything, so the
// run after the record's removal sets up and commits everything.
func TestInitRefusesAClonedRecordBeforeWritingAnything(t *testing.T) {
	repo := initRepo(t)
	writeRepoFile(t, filepath.Join(repo, ".env"), "TOKEN=x\n")
	writeRepoFile(t, uncommittedRecord(repo), ".env\n")
	gitIn(t, repo, "add", "-f", uncommittedRecord(repo))
	gitIn(t, repo, "commit", "-q", "-m", "chore: ship a record")

	err := runInit(t, firstInit...)

	if err == nil || !strings.Contains(err.Error(), "does not trust it") {
		t.Fatalf("init should refuse a record git tracks, got %v", err)
	}
	if status := gitIn(t, repo, "status", "--porcelain", "--untracked-files=all"); status != "?? .env\n" {
		t.Errorf("a refused record should stop init before it writes anything:\n%s", status)
	}

	if err := os.Remove(uncommittedRecord(repo)); err != nil {
		t.Fatal(err)
	}
	mustInit(t, firstInit...)

	if status := gitIn(t, repo, "status", "--porcelain", "--untracked-files=all"); status != " D .sdd/tmp/init-uncommitted\n?? .env\n" {
		t.Errorf("the run after the record's removal should commit its whole setup and nothing else:\n%s", status)
	}
}

func TestInitRefusesARecordItCannotTrust(t *testing.T) {
	cases := []struct {
		name    string
		record  string
		prepare func(t *testing.T, repo string)
	}{
		{name: "the repository root", record: ".\n"},
		{name: "a path outside the repository", record: "../outside.txt\n"},
		{name: "an absolute path", record: "/etc/hosts\n"},
		{name: "an unclean path", record: ".claude/../.env\n"},
		{
			name:   "a record git tracks",
			record: ".env\n",
			prepare: func(t *testing.T, repo string) {
				gitIn(t, repo, "add", "-f", uncommittedRecord(repo))
				gitIn(t, repo, "commit", "-q", "-m", "chore: ship a record")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := initRepo(t)
			mustInit(t, firstInit...)
			writeRepoFile(t, filepath.Join(repo, ".env"), "TOKEN=x\n")
			writeRepoFile(t, uncommittedRecord(repo), tc.record)
			if tc.prepare != nil {
				tc.prepare(t, repo)
			}
			head := headCommit(t, repo)

			err := runInit(t)

			if err == nil || !strings.Contains(err.Error(), uncommittedRecord(repo)) {
				t.Errorf("init should refuse a record it cannot trust, naming it, got %v", err)
			}
			if headCommit(t, repo) != head {
				t.Error("a refused record should commit nothing")
			}
		})
	}
}
