package cliapp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestInitSetUpCommitKeepsItsSubject(t *testing.T) {
	repo := setUpInitRepo(t)

	if subject := headSubject(t, repo); subject != "sdd: init .sdd/ metadata directory" {
		t.Errorf("a set-up should commit under its own subject, got %q", subject)
	}
}

func TestInitRefreshOfConfigNamesConfig(t *testing.T) {
	repo := setUpInitRepo(t)

	runInit(t, repo, "--verify-mode", "prose")

	if subject := headSubject(t, repo); subject != "sdd: init refresh: config" {
		t.Errorf("a refresh writing only config should say so, got %q", subject)
	}
}

func TestInitRefreshOfSkillsAndIgnoreFilesNamesBoth(t *testing.T) {
	repo := setUpInitRepo(t)
	older := model.SkillBundleEntry{Skill: "sdd", RelPath: "SKILL.md", Content: []byte("---\nname: sdd\ndescription: An older render.\n---\nOlder.\n")}
	rendered, err := model.RenderSkillFile(older, "v0.1.0", model.ComputeSkillHash(older.Content))
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(repo, ".claude", "skills", "sdd", "SKILL.md"), rendered, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, ".sdd", ".gitignore"), "tmp/\n")
	gitIn(t, repo, "commit", "-q", "-am", "an older setup")

	runInit(t, repo)

	if subject := headSubject(t, repo); subject != "sdd: init refresh: skills, ignore files" {
		t.Errorf("a refresh of a skill and an ignore file should name both, got %q", subject)
	}
}
