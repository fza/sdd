package cliapp_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitRestoresASkillIdenticalToHeadWithoutACommit(t *testing.T) {
	repo := canonicalTemp(t)
	initRepo(t, repo)
	runApp(t, repo, "init", "--scope", "project", "--agents", "claude", "--language", "en", "--participant", "Ada")
	skill := filepath.Join(repo, ".claude", "skills", "sdd", "SKILL.md")
	err := os.Remove(skill)
	if err != nil {
		t.Fatal(err)
	}
	head := gitIn(t, repo, "rev-parse", "HEAD")

	runApp(t, repo, "init")

	if _, statErr := os.Stat(skill); statErr != nil {
		t.Errorf("init should restore the deleted skill: %v", statErr)
	}
	if gitIn(t, repo, "rev-parse", "HEAD") != head {
		t.Error("a skill restored identical to HEAD should need no commit")
	}
	if status := gitIn(t, repo, "status", "--porcelain"); status != "" {
		t.Errorf("the restored skill should match HEAD:\n%s", status)
	}
	if _, statErr := os.Stat(uncommittedRecord(repo)); !os.IsNotExist(statErr) {
		t.Errorf("a run with nothing to commit should leave no record: %v", statErr)
	}
}
