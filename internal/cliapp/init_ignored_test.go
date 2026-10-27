package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCommitsAroundIgnoredAgentSurfaces(t *testing.T) {
	repo := canonicalTemp(t)
	initRepo(t, repo)
	writeFile(t, filepath.Join(repo, ".gitignore"), ".claude/\n")
	gitIn(t, repo, "add", ".gitignore")
	gitIn(t, repo, "commit", "-q", "-m", "ignore agent surfaces")

	runApp(t, repo, "init", "--scope", "project", "--agents", "claude", "--language", "en", "--participant", "Ada")

	skill := filepath.Join(repo, ".claude", "skills", "sdd", "SKILL.md")
	if _, err := os.Stat(skill); err != nil {
		t.Fatalf("init should install the skills even where git ignores them: %v", err)
	}
	if status := gitIn(t, repo, "status", "--porcelain"); status != "" {
		t.Errorf("init should commit everything it may and leave nothing staged:\n%s", status)
	}
	if committed := gitIn(t, repo, "ls-files", ".claude"); committed != "" {
		t.Errorf("init should not force ignored skills into the commit:\n%s", committed)
	}
	if !strings.Contains(gitIn(t, repo, "ls-files", ".sdd"), ".sdd/config.yaml") {
		t.Error("init should commit its config")
	}

	err := os.Remove(skill)
	if err != nil {
		t.Fatal(err)
	}
	runApp(t, repo, "init")
	if _, err := os.Stat(skill); err != nil {
		t.Errorf("a repeat init should restore the skill: %v", err)
	}
	if status := gitIn(t, repo, "status", "--porcelain"); status != "" {
		t.Errorf("a repeat init should leave nothing staged:\n%s", status)
	}
}
