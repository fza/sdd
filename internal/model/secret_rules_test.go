package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestIgnoredSettingsReadsSecretRulesUpdateFromTheGlobalConfigOnly(t *testing.T) {
	cases := []struct {
		name   string
		value  model.SecretRulesUpdate
		source model.ConfigValueSource
		reason string
	}{
		{name: "global daily", value: model.SecretRulesDaily, source: model.ConfigSourceGlobal},
		{name: "global off", value: model.SecretRulesOff, source: model.ConfigSourceGlobal},
		{name: "global unknown value", value: "weekly", source: model.ConfigSourceGlobal, reason: `"weekly" is not daily or off`},
		{name: "committed config", value: model.SecretRulesOff, source: model.ConfigSourceProject, reason: "only the user-global config may set it"},
		{name: "local config", value: model.SecretRulesOff, source: model.ConfigSourceLocal, reason: "only the user-global config may set it"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ignored := model.IgnoredSettings(model.BaseConfig{Secrets: model.SecretsConfig{RulesUpdate: tc.value}}, tc.source)

			if tc.reason == "" {
				if len(ignored) != 0 {
					t.Errorf("the setting should be honored, got %v", ignored)
				}
				return
			}
			if len(ignored) != 1 || ignored[0].Key != "secrets.rules_update" || ignored[0].Reason != tc.reason {
				t.Errorf("the setting should be reported as ignored, saying why, got %v", ignored)
			}
		})
	}
}

func TestResolveSecretRulesUpdate(t *testing.T) {
	cases := []struct {
		name  string
		value model.SecretRulesUpdate
		want  model.SecretRulesUpdate
	}{
		{name: "unset", want: model.SecretRulesDaily},
		{name: "daily", value: model.SecretRulesDaily, want: model.SecretRulesDaily},
		{name: "off", value: model.SecretRulesOff, want: model.SecretRulesOff},
		{name: "unknown value", value: "weekly", want: model.SecretRulesDaily},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := model.ResolveSecretRulesUpdate(model.SecretsConfig{RulesUpdate: tc.value})

			if got != tc.want {
				t.Errorf("the effective setting should follow the value or fall back to the default, got %q", got)
			}
		})
	}
}
