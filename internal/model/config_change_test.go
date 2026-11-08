package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestConfigChanges(t *testing.T) {
	before := model.ConfigLayers{
		Global: model.BaseConfig{LLM: model.LLMConfig{APIKeys: map[string]string{"anthropic": "old-secret"}}},
		Local:  &model.PerRepoConfig{BaseConfig: model.BaseConfig{Participant: "Ada"}},
	}
	after := model.ConfigLayers{
		Global:  model.BaseConfig{LLM: model.LLMConfig{APIKeys: map[string]string{"anthropic": "new-secret"}}},
		Local:   &model.PerRepoConfig{BaseConfig: model.BaseConfig{Participant: "Ada", Dialogue: model.DialogueConfig{Mode: model.DialogueAutonomous}}},
		Pointer: &model.PerRepoConfig{ProjectDir: "../graph"},
	}

	changes := model.ConfigChanges(before, after)
	byKey := map[string]model.ConfigChange{}
	for _, change := range changes {
		byKey[change.Key] = change
	}
	if got := byKey["dialogue.mode"]; got.Old != "guided" || got.New != "autonomous" {
		t.Errorf("a changed key should carry its old and new value, got %+v", got)
	}
	if got, ok := byKey["llm.api_keys.anthropic"]; !ok || !got.Secret || got.Old != "" || got.New != "" {
		t.Errorf("a changed secret should name its key and carry no value, got %+v (found %v)", got, ok)
	}
	if got := byKey["pointer project_dir"]; got.New != "../graph" {
		t.Errorf("a changed pointer should be listed, got %+v", got)
	}
	if _, ok := byKey["participant"]; ok {
		t.Error("an unchanged key should not be listed")
	}

	notice := model.FormatConfigNotice(changes)
	for _, want := range []string{"dialogue.mode: guided -> autonomous", "llm.api_keys.anthropic changed"} {
		if !strings.Contains(notice, want) {
			t.Errorf("the notice should say %q:\n%s", want, notice)
		}
	}
	for _, secret := range []string{"old-secret", "new-secret"} {
		if strings.Contains(notice, secret) {
			t.Errorf("the notice must not show a secret value:\n%s", notice)
		}
	}
	if model.FormatConfigNotice(model.ConfigChanges(after, after)) != "" {
		t.Error("an unchanged config should carry no notice")
	}
}
