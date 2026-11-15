package cliapp_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitRestoresASkillIdenticalToHeadWithoutACommit(t *testing.T) {
	repo := initRepo(t)
	mustInit(t, firstInit...)
	skill := filepath.Join(repo, ".claude", "skills", "sdd", "SKILL.md")
	if err := os.Remove(skill); err != nil {
		t.Fatal(err)
	}
	head := headCommit(t, repo)

	mustInit(t)

	if _, statErr := os.Stat(skill); statErr != nil {
		t.Errorf("init should restore the deleted skill: %v", statErr)
	}
	if headCommit(t, repo) != head {
		t.Error("a skill restored identical to HEAD should need no commit")
	}
	if status := gitIn(t, repo, "status", "--porcelain"); status != "" {
		t.Errorf("the restored skill should match HEAD:\n%s", status)
	}
	if _, statErr := os.Stat(uncommittedRecord(repo)); !os.IsNotExist(statErr) {
		t.Errorf("a run with nothing to commit should leave no record: %v", statErr)
	}
}
