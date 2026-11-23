package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestConfigChanges(t *testing.T) {
	before := model.ConfigLayers{
		Global:  model.BaseConfig{LLM: model.LLMConfig{APIKeys: map[string]string{"anthropic": "old-secret", "openai": "kept-secret"}}},
		Project: &model.PerRepoConfig{Language: "de", DefaultBranch: "main"},
	}
	after := model.ConfigLayers{
		Global:  model.BaseConfig{LLM: model.LLMConfig{APIKeys: map[string]string{"anthropic": "new-secret", "openai": "kept-secret"}}},
		Project: &model.PerRepoConfig{Language: "en", DefaultBranch: "main"},
		Local:   &model.PerRepoConfig{BaseConfig: model.BaseConfig{Participant: "Ada"}},
	}

	changes := model.ConfigChanges(before, after)
	byKey := map[string]model.ConfigChange{}
	var keys []string
	for _, change := range changes {
		byKey[change.Key] = change
		keys = append(keys, change.Key)
	}
	if got := byKey["language"]; got.Old != "de" || got.New != "en" || got.Secret {
		t.Errorf("a changed key should carry its old and new value, got %+v", got)
	}
	if got := byKey["participant"]; got.Old != "" || got.New != "Ada" {
		t.Errorf("a key set only after should carry no old value, got %+v", got)
	}
	if got, ok := byKey["llm.api_keys.anthropic"]; !ok || !got.Secret || got.Old != "" || got.New != "" {
		t.Errorf("a changed secret should name its key and carry no value, got %+v (found %v)", got, ok)
	}
	for _, unchanged := range []string{"default_branch", "llm.api_keys.openai", "llm.provider"} {
		if _, ok := byKey[unchanged]; ok {
			t.Errorf("an unchanged key should not be listed: %s", unchanged)
		}
	}
	if strings.Join(keys, ",") != "llm.api_keys.anthropic,participant,language" {
		t.Errorf("changes should follow schema order, got %v", keys)
	}

	notice := model.FormatConfigNotice(changes)
	want := "config changed, now in force:\n" +
		"- llm.api_keys.anthropic changed (secret, value not shown)\n" +
		"- participant: (unset) -> Ada\n" +
		"- language: de -> en"
	if notice != want {
		t.Errorf("the notice should list each change, a secret by key only:\n%s", notice)
	}
	for _, secret := range []string{"old-secret", "new-secret", "kept-secret"} {
		if strings.Contains(notice, secret) {
			t.Errorf("the notice must not show a secret value:\n%s", notice)
		}
	}
	if notice := model.FormatConfigNotice(model.ConfigChanges(after, after)); notice != "" {
		t.Errorf("an unchanged config should carry no notice, got %q", notice)
	}
}
