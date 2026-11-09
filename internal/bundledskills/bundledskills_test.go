package bundledskills_test

import (
	"bytes"
	"testing"

	"github.com/networkteam/sdd/internal/bundledskills"
	"github.com/networkteam/sdd/internal/model"
)

// TestLoadShipsOnlyTheEngineSkill pins the v0.18.0 bundle shape (d-tac-ip1):
// one skill, `sdd`, opening the engine flow. The skill names no reference, so
// none ships with it; the locale vocabulary is engine data (d-cpt-xob).
func TestLoadShipsOnlyTheEngineSkill(t *testing.T) {
	for _, target := range []model.AgentTarget{model.AgentClaude, model.AgentCodex} {
		b, err := bundledskills.Load(target)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		var paths []string
		for _, e := range b.Entries {
			if e.Skill != "sdd" {
				t.Errorf("%s: legacy skill %q still in the bundle", target, e.Skill)
			}
			paths = append(paths, e.RelPath)
		}
		want := []string{"SKILL.md"}
		if len(paths) != len(want) {
			t.Fatalf("%s: bundle paths = %v, want %v", target, paths, want)
		}
		for i := range want {
			if paths[i] != want[i] {
				t.Errorf("%s: bundle paths = %v, want %v", target, paths, want)
			}
		}
	}
}

func TestVocabulary_German(t *testing.T) {
	body, err := bundledskills.Vocabulary("de")
	if err != nil {
		t.Fatalf("Vocabulary: %v", err)
	}
	if !bytes.HasPrefix(body, []byte("# SDD Vokabular")) {
		t.Errorf("vocabulary must open with its heading, no frontmatter:\n%.80s", body)
	}
	if !bytes.Contains(body, []byte("Entscheidung")) {
		t.Errorf("German vocabulary missing expected terms:\n%s", body)
	}
}

func TestVocabulary_MissingLocale(t *testing.T) {
	if _, err := bundledskills.Vocabulary("fr"); err == nil {
		t.Fatal("expected an error for a locale the bundle does not ship")
	}
}

// TestLoadRendersProfiles checks that both render profiles resolve every
// template action, that the skill opens the engine flow, and that each
// profile's frontmatter matches its host: Claude carries no Agent-Skills
// compatibility field, Codex does.
func TestLoadRendersProfiles(t *testing.T) {
	badMarkers := []string{"{{ inject", "{{inject", "{{ if", "{{if", "{{ template", "{{template", "{{ end", "{{ else"}
	for _, target := range []model.AgentTarget{model.AgentClaude, model.AgentCodex} {
		b, err := bundledskills.Load(target)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		var skill []byte
		for _, e := range b.Entries {
			for _, m := range badMarkers {
				if bytes.Contains(e.Content, []byte(m)) {
					t.Errorf("%s: %s/%s: unrendered template action %q survived render", target, e.Skill, e.RelPath, m)
				}
			}
			if e.RelPath == "SKILL.md" {
				skill = e.Content
			}
		}
		if !bytes.HasPrefix(skill, []byte("---\nname: sdd\n")) {
			t.Errorf("%s: SKILL.md must be named sdd:\n%.80s", target, skill)
		}
		if !bytes.Contains(skill, []byte("`start_session`")) {
			t.Errorf("%s: SKILL.md does not open the engine flow", target)
		}
		for _, stale := range []string{"sdd-engine", "sdd-catchup", "sdd-bootstrap", "deprecated", "Deprecated"} {
			if bytes.Contains(skill, []byte(stale)) {
				t.Errorf("%s: SKILL.md still names %q", target, stale)
			}
		}
		hasCompat := bytes.Contains(skill, []byte("compatibility: Designed for OpenAI Codex"))
		if target == model.AgentCodex && !hasCompat {
			t.Errorf("codex: SKILL.md missing the compatibility field")
		}
		if target == model.AgentClaude && hasCompat {
			t.Errorf("claude: SKILL.md must not carry a compatibility field")
		}
	}
}
