package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestParseConfigClaudeCLICredentials(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
		want model.ClaudeCLICredentials
	}{
		{"absent", "llm:\n  provider: claude-cli\n", ""},
		{"login", "llm:\n  claude_cli:\n    credentials: login\n", model.ClaudeCLICredentialsLogin},
		{"env", "llm:\n  claude_cli:\n    credentials: env\n", model.ClaudeCLICredentialsEnv},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := model.ParseConfig([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("ParseConfig: %v", err)
			}
			if cfg.LLM.ClaudeCLI.Credentials != tc.want {
				t.Errorf("credentials should decode to the written value, got %q", cfg.LLM.ClaudeCLI.Credentials)
			}
		})
	}
}

func TestParseConfigRefusesUnknownClaudeCLICredentials(t *testing.T) {
	_, err := model.ParseConfig([]byte("llm:\n  claude_cli:\n    credentials: apikey\n"))
	if err == nil {
		t.Fatal("an unknown credentials value should fail the load")
	}
	for _, want := range []string{`"apikey"`, "login", "env"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should name the value and the allowed ones, got %v", err)
		}
	}
}

func TestEffectiveConfigShowsClaudeCLICredentialsDefault(t *testing.T) {
	for _, v := range model.EffectiveConfigValues(model.BaseConfig{}, nil, nil) {
		if v.Key != "llm.claude_cli.credentials" {
			continue
		}
		if v.Value != string(model.ClaudeCLICredentialsLogin) || v.Source != model.ConfigSourceDefault {
			t.Errorf("an unset credentials key should show the login default, got %+v", v)
		}
		return
	}
	t.Error("the effective config should list llm.claude_cli.credentials")
}
