package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// refuseCommits points repo at a commit-msg hook refusing every commit, and
// returns the call that lifts it.
func refuseCommits(t *testing.T, repo string) (allow func()) {
	t.Helper()
	hooks := canonicalTemp(t)
	hook := filepath.Join(hooks, "commit-msg")
	writeFile(t, hook, "#!/bin/sh\necho 'commit refused' >&2\nexit 1\n")
	err := os.Chmod(hook, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "config", "core.hooksPath", hooks)
	return func() { gitIn(t, repo, "config", "--unset", "core.hooksPath") }
}

func uncommittedRecord(repo string) string {
	return filepath.Join(repo, ".sdd", "tmp", "init-uncommitted")
}

func TestInitCompletesTheCommitAnEarlierRunLeftUndone(t *testing.T) {
	repo := canonicalTemp(t)
	initRepo(t, repo)
	allow := refuseCommits(t, repo)

	_, err := runAppErr(t, repo, "init", "--scope", "project", "--agents", "claude", "--language", "en", "--participant", "Ada")
	if err == nil {
		t.Fatal("init should fail when its commit is refused")
	}
	if _, statErr := os.Stat(uncommittedRecord(repo)); statErr != nil {
		t.Fatalf("a refused commit should leave a record of the paths it held: %v", statErr)
	}
	_, err = runAppErr(t, repo, "init")
	if err == nil {
		t.Fatal("a re-run whose commit is refused again should fail")
	}
	if _, statErr := os.Stat(uncommittedRecord(repo)); statErr != nil {
		t.Fatalf("a re-run refused again should keep the record: %v", statErr)
	}

	allow()
	runApp(t, repo, "init")

	if status := gitIn(t, repo, "status", "--porcelain"); status != "" {
		t.Errorf("a re-run should commit what the refused runs left uncommitted:\n%s", status)
	}
	if !strings.HasPrefix(headSubject(t, repo), "sdd: ") {
		t.Error("the completed commit should be init's own")
	}
	if _, statErr := os.Stat(uncommittedRecord(repo)); !os.IsNotExist(statErr) {
		t.Errorf("a landed commit should remove the record: %v", statErr)
	}
}

func TestInitLeavesTheUsersOwnEditsOutOfARetriedCommit(t *testing.T) {
	repo := canonicalTemp(t)
	initRepo(t, repo)
	runApp(t, repo, "init", "--scope", "project", "--agents", "claude", "--language", "en", "--participant", "Ada")
	gitignore := filepath.Join(repo, ".gitignore")
	writeFile(t, gitignore, readFile(t, gitignore)+"/scratch\n")
	skill := filepath.Join(repo, ".claude", "skills", "sdd", "SKILL.md")
	err := os.Remove(skill)
	if err != nil {
		t.Fatal(err)
	}
	allow := refuseCommits(t, repo)
	_, err = runAppErr(t, repo, "init")
	if err == nil {
		t.Fatal("init should fail when its commit is refused")
	}
	allow()

	runApp(t, repo, "init")

	if status := gitIn(t, repo, "status", "--porcelain"); status != " M .gitignore\n" {
		t.Errorf("the retried commit should take the restored skill and leave the user's own edit uncommitted:\n%s", status)
	}
	if _, statErr := os.Stat(uncommittedRecord(repo)); !os.IsNotExist(statErr) {
		t.Errorf("a landed commit should remove the record: %v", statErr)
	}
}

func TestInitDropsRecordedPathsCommittedMeanwhile(t *testing.T) {
	repo := canonicalTemp(t)
	initRepo(t, repo)
	allow := refuseCommits(t, repo)
	_, err := runAppErr(t, repo, "init", "--scope", "project", "--agents", "claude", "--language", "en", "--participant", "Ada")
	if err == nil {
		t.Fatal("init should fail when its commit is refused")
	}
	allow()
	gitIn(t, repo, "add", "--all")
	gitIn(t, repo, "commit", "-q", "-m", "chore: commit the setup by hand")
	head := gitIn(t, repo, "rev-parse", "HEAD")

	runApp(t, repo, "init")

	if gitIn(t, repo, "rev-parse", "HEAD") != head {
		t.Error("a re-run with nothing left uncommitted should commit nothing")
	}
	if _, statErr := os.Stat(uncommittedRecord(repo)); !os.IsNotExist(statErr) {
		t.Errorf("a record with nothing left to commit should be removed: %v", statErr)
	}
}

func TestInitCompletesARefusedCommitInTheProjectRepository(t *testing.T) {
	work, project := workRepository(t)
	runApp(t, work, "init", "--project-dir", "../work-sdd", "--graph-dir", "graph", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude")
	skill := filepath.Join(project, ".claude", "skills", "sdd", "SKILL.md")
	gitIn(t, project, "rm", "-q", skill)
	gitIn(t, project, "commit", "-q", "-m", "chore: drop the skill")
	allow := refuseCommits(t, project)
	_, err := runAppErr(t, work, "init")
	if err == nil {
		t.Fatal("init should fail when the project's commit is refused")
	}
	allow()

	runApp(t, work, "init")

	if status := gitIn(t, project, "status", "--porcelain"); status != "" {
		t.Errorf("a re-run should commit what the refused run left uncommitted in the project:\n%s", status)
	}
	if _, statErr := os.Stat(uncommittedRecord(project)); !os.IsNotExist(statErr) {
		t.Errorf("a landed commit should remove the project's record: %v", statErr)
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
			repo := canonicalTemp(t)
			initRepo(t, repo)
			runApp(t, repo, "init", "--scope", "project", "--agents", "claude", "--language", "en", "--participant", "Ada")
			writeFile(t, filepath.Join(repo, ".env"), "TOKEN=x\n")
			writeFile(t, uncommittedRecord(repo), tc.record)
			if tc.prepare != nil {
				tc.prepare(t, repo)
			}
			head := gitIn(t, repo, "rev-parse", "HEAD")

			_, err := runAppErr(t, repo, "init")

			if err == nil {
				t.Error("init should refuse a record it cannot trust")
			}
			if gitIn(t, repo, "rev-parse", "HEAD") != head {
				t.Error("a refused record should commit nothing")
			}
		})
	}
}
