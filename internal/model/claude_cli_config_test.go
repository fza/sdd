package model_test

import (
	"reflect"
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

func TestParseClaudeCLICredentials(t *testing.T) {
	for value, want := range map[string]model.ClaudeCLICredentials{
		"":      model.ClaudeCLICredentialsLogin,
		"login": model.ClaudeCLICredentialsLogin,
		"env":   model.ClaudeCLICredentialsEnv,
	} {
		got, err := model.ParseClaudeCLICredentials(value)
		if err != nil || got != want {
			t.Errorf("ParseClaudeCLICredentials(%q) = %q, %v; want %q", value, got, err, want)
		}
	}
	_, err := model.ParseClaudeCLICredentials("apikey")
	if err == nil {
		t.Fatal("an unknown credentials value should be refused, not read as login")
	}
	for _, want := range []string{`"apikey"`, "login", "env"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should name the value and the allowed ones, got %v", err)
		}
	}
}

func TestConfigSetRefusesUnknownClaudeCLICredentials(t *testing.T) {
	err := model.ConfigValueRefusal(reflect.TypeFor[model.PerRepoConfig](), []string{"llm", "claude_cli", "credentials"}, "apikey")
	if err == nil || !strings.Contains(err.Error(), "login or env") {
		t.Errorf("config set should refuse an unknown credentials value, naming the allowed ones, got %v", err)
	}
}

func TestMergeConfigClaudeCLICredentials(t *testing.T) {
	base := model.BaseConfig{LLM: model.LLMConfig{ClaudeCLI: model.ClaudeCLIConfig{Credentials: model.ClaudeCLICredentialsEnv}}}
	if got := model.MergeBaseConfig(base, model.BaseConfig{}).LLM.ClaudeCLI.Credentials; got != model.ClaudeCLICredentialsEnv {
		t.Errorf("an unset overlay should keep the base's credentials, got %q", got)
	}
	overlay := model.BaseConfig{LLM: model.LLMConfig{ClaudeCLI: model.ClaudeCLIConfig{Credentials: model.ClaudeCLICredentialsLogin}}}
	if got := model.MergeBaseConfig(base, overlay).LLM.ClaudeCLI.Credentials; got != model.ClaudeCLICredentialsLogin {
		t.Errorf("the overlay's credentials should win, got %q", got)
	}
}

// A generate prefilter on claude-cli runs on the account the llm block's calls
// run on, unless llm_prefilter sets its own credentials.
func TestResolvedPrefilterTakesClaudeCLICredentials(t *testing.T) {
	llm := model.LLMConfig{Provider: "claude-cli", ClaudeCLI: model.ClaudeCLIConfig{Credentials: model.ClaudeCLICredentialsEnv}}
	if got := (model.BaseConfig{LLM: llm}).ResolvedPrefilter().ClaudeCLI.Credentials; got != model.ClaudeCLICredentialsEnv {
		t.Errorf("the derived prefilter should take the llm block's credentials, got %q", got)
	}
	own := model.LLMPrefilterConfig{LLMConfig: model.LLMConfig{ClaudeCLI: model.ClaudeCLIConfig{Credentials: model.ClaudeCLICredentialsLogin}}}
	if got := (model.BaseConfig{LLM: llm, LLMPrefilter: own}).ResolvedPrefilter().ClaudeCLI.Credentials; got != model.ClaudeCLICredentialsLogin {
		t.Errorf("credentials llm_prefilter sets should win, got %q", got)
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
