package handlers_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
)

// TestInstallSkills_FreshInstall verifies that every embedded entry ends up
// in the "installed" category when the target dir starts empty.
func TestInstallSkills_FreshInstall(t *testing.T) {
	tmp := t.TempDir()

	h := handlers.New(handlers.Options{
		Reader: finders.New(finders.Options{}),
	})

	var got command.SkillInstallResult
	err := h.InstallSkills(context.Background(), &command.InstallSkillsCmd{
		Target:        model.AgentClaude,
		Scope:         model.ScopeProject,
		RepoRoot:      tmp,
		BinaryVersion: "v0.2.0",
		OnInstalled: func(r command.SkillInstallResult) {
			got = r
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Installed) == 0 {
		t.Fatal("expected files to be installed")
	}
	if len(got.Refreshed)+len(got.Overwritten)+len(got.SkippedModified)+len(got.Current) != 0 {
		t.Errorf("unexpected non-installed categories: %+v", got)
	}

	// Spot-check one installed file was actually written with stamps.
	for _, p := range got.Installed {
		if !strings.HasPrefix(p, filepath.Join(tmp, ".claude", "skills")+string(os.PathSeparator)) && p != filepath.Join(tmp, ".claude", "agents", "sdd_delegate.md") {
			t.Errorf("installed path not under expected dir: %s", p)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		sf := model.ParseSkillFile(p, data)
		if sf.StoredVersion != "v0.2.0" {
			t.Errorf("%s: StoredVersion = %q, want v0.2.0", p, sf.StoredVersion)
		}
		if sf.StoredHash == "" {
			t.Errorf("%s: StoredHash empty", p)
		}
	}
}

// TestInstallSkills_RepeatIsNoOp verifies that a second install run classifies
// everything as Current and writes nothing.
func TestInstallSkills_RepeatIsNoOp(t *testing.T) {
	tmp := t.TempDir()
	h := handlers.New(handlers.Options{Reader: finders.New(finders.Options{})})

	run := func() command.SkillInstallResult {
		var got command.SkillInstallResult
		err := h.InstallSkills(context.Background(), &command.InstallSkillsCmd{
			Target:        model.AgentClaude,
			Scope:         model.ScopeProject,
			RepoRoot:      tmp,
			BinaryVersion: "v0.2.0",
			OnInstalled:   func(r command.SkillInstallResult) { got = r },
		})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	_ = run() // fresh install
	second := run()
	if len(second.Installed)+len(second.Refreshed)+len(second.Overwritten)+len(second.SkippedModified) != 0 {
		t.Errorf("second run should be a no-op, got: %+v", second)
	}
	if len(second.Current) == 0 {
		t.Errorf("second run should classify everything as current")
	}
}

// TestInstallSkills_PromptModified verifies the prompt callback is consulted
// when a file has been user-edited, and routes outcomes to Overwritten vs
// SkippedModified based on its return value.
func TestInstallSkills_PromptModified(t *testing.T) {
	tmp := t.TempDir()
	h := handlers.New(handlers.Options{Reader: finders.New(finders.Options{})})

	// Fresh install so files exist.
	err := h.InstallSkills(context.Background(), &command.InstallSkillsCmd{
		Target:        model.AgentClaude,
		Scope:         model.ScopeProject,
		RepoRoot:      tmp,
		BinaryVersion: "v0.2.0",
	})
	if err != nil {
		t.Fatal(err)
	}

	edited := filepath.Join(tmp, ".claude", "skills", "sdd", "SKILL.md")
	data, err := os.ReadFile(edited)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(edited, append(data, []byte("\nuser edit\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	// The user rejects first, then approves.
	install := func(approve bool) command.SkillInstallResult {
		t.Helper()
		var got command.SkillInstallResult
		err := h.InstallSkills(context.Background(), &command.InstallSkillsCmd{
			Target:        model.AgentClaude,
			Scope:         model.ScopeProject,
			RepoRoot:      tmp,
			BinaryVersion: "v0.2.0",
			PromptOverwrite: func(p string) (bool, error) {
				return approve && p == edited, nil
			},
			OnInstalled: func(r command.SkillInstallResult) { got = r },
		})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	rejected := install(false)
	if !slices.Contains(rejected.SkippedModified, edited) || len(rejected.Overwritten) != 0 {
		t.Errorf("a rejected prompt should skip the edited file, got: %+v", rejected)
	}
	approved := install(true)
	if !slices.Contains(approved.Overwritten, edited) {
		t.Errorf("an approved prompt should overwrite the edited file, got: %+v", approved)
	}
}
