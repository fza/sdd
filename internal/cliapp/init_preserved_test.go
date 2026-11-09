package cliapp_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestInitThatKeepsAnEditedSkillPrintsItsFullReport(t *testing.T) {
	repo := setUpInitRepo(t)
	skill := filepath.Join(repo, ".claude", "skills", "sdd", "SKILL.md")
	writeFile(t, skill, readText(t, skill)+"An edit of our own.\n")
	gitIn(t, repo, "commit", "-q", "-am", "an edited skill")
	head := headSubject(t, repo)

	out := runInit(t, repo)

	if !strings.Contains(out, "  preserved: 1 modified file(s) left untouched\n    - "+skill+"\n") || strings.Contains(out, nothingChanged) {
		t.Errorf("an init keeping an edited skill should print its full report, got:\n%s", out)
	}
	if headSubject(t, repo) != head {
		t.Error("an init keeping an edited skill should commit nothing")
	}
}

func TestInitThatKeepsAnEditedOrphanPrintsItsFullReport(t *testing.T) {
	repo := setUpInitRepo(t)
	entry := model.SkillBundleEntry{Skill: "sdd", RelPath: "references/gone.md", Content: []byte("---\nname: gone\n---\nA reference the bundle dropped.\n")}
	rendered, err := model.RenderSkillFile(entry, "v0.1.0", model.ComputeSkillHash(entry.Content))
	if err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(repo, ".claude", "skills", "sdd", "references", "gone.md")
	writeFile(t, orphan, string(rendered)+"An edit of our own.\n")
	gitIn(t, repo, "add", orphan)
	gitIn(t, repo, "commit", "-q", "-m", "an edited skill file the bundle dropped")

	out := runInit(t, repo)

	if !strings.Contains(out, "(pass --force to remove)\n    - "+orphan+"\n") || strings.Contains(out, nothingChanged) {
		t.Errorf("an init keeping an edited orphan should print its full report, got:\n%s", out)
	}
}
