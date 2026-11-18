package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func effective(values []model.ConfigValue) map[string]model.ConfigValue {
	byKey := make(map[string]model.ConfigValue, len(values))
	for _, v := range values {
		byKey[v.Key] = v
	}
	return byKey
}

// `sdd config` shows the defaults a run uses for the verify block and the
// llm effort.
func TestEffectiveConfigValues_ShowVerifyAndEffortDefaults(t *testing.T) {
	values := effective(model.EffectiveConfigValues(model.BaseConfig{}, nil, nil))
	for key, want := range map[string]string{
		"verify.mode":           "prose",
		"verify.prefilter":      "true",
		"verify.auto_calibrate": "true",
		"verify.runner":         "subagent",
		"verify.claude.model":   "sonnet",
		"verify.claude.effort":  "medium",
		"llm.effort":            "medium",
	} {
		if got := values[key]; got.Value != want || got.Source != model.ConfigSourceDefault {
			t.Errorf("%s = %+v, want the default %s", key, got, want)
		}
	}
}

func TestEffectiveConfigValues_PrefilterSetOffShowsFalse(t *testing.T) {
	off := false
	project := &model.PerRepoConfig{}
	project.Verify.Prefilter = &off
	if got := effective(model.EffectiveConfigValues(model.BaseConfig{}, project, nil))["verify.prefilter"]; got.Value != "false" || got.Source != model.ConfigSourceProject {
		t.Fatalf("a prefilter switched off should show false from the project, got %+v", got)
	}
}

// The llm defaults follow the provider: codex runs its own default model, and
// only the CLI providers take an effort.
func TestEffectiveConfigValues_LLMDefaultsFollowTheProvider(t *testing.T) {
	codex := effective(model.EffectiveConfigValues(model.BaseConfig{LLM: model.LLMConfig{Provider: "codex"}}, nil, nil))
	if got, shown := codex["llm.model"]; shown {
		t.Errorf("codex should show no default llm.model, got %+v", got)
	}
	if got := codex["llm.effort"]; got.Value != "medium" {
		t.Errorf("codex should show the default effort, got %+v", got)
	}
	anthropic := effective(model.EffectiveConfigValues(model.BaseConfig{LLM: model.LLMConfig{Provider: "anthropic"}}, nil, nil))
	if got, shown := anthropic["llm.effort"]; shown {
		t.Errorf("an API provider takes no llm.effort, got %+v", got)
	}
	if got := anthropic["llm.model"]; got.Value != model.DefaultLLMModel {
		t.Errorf("anthropic should show the default model, got %+v", got)
	}
}

func TestGroundingPolicy_PrefilterOnByDefault(t *testing.T) {
	if !(model.VerifyConfig{}).GroundingPolicy().Prefilter {
		t.Fatal("an unset verify.prefilter should be on")
	}
	off := false
	if (model.VerifyConfig{Prefilter: &off}).GroundingPolicy().Prefilter {
		t.Fatal("verify.prefilter false should switch it off")
	}
}

// verify.auto_calibrate is on unless a layer switches it off, and the
// machine-local layer wins.
func TestGroundingPolicy_AutoCalibrateOnByDefault(t *testing.T) {
	if !(model.VerifyConfig{}).GroundingPolicy().AutoCalibrate {
		t.Fatal("an unset verify.auto_calibrate should be on")
	}
	off, on := false, true
	project := &model.PerRepoConfig{}
	project.Verify.AutoCalibrate = &on
	local := &model.PerRepoConfig{}
	local.Verify.AutoCalibrate = &off
	got := effective(model.EffectiveConfigValues(model.BaseConfig{}, project, local))["verify.auto_calibrate"]
	if got.Value != "false" || got.Source != model.ConfigSourceLocal {
		t.Fatalf("verify.auto_calibrate = %+v: the local layer should switch it off", got)
	}
}

func TestLLMConfig_ResolvedModel(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg  model.LLMConfig
		want string
	}{
		"default provider":   {model.LLMConfig{}, model.DefaultLLMModel},
		"codex leaves empty": {model.LLMConfig{Provider: "codex"}, ""},
		"set model wins":     {model.LLMConfig{Provider: "codex", Model: "gpt-5.5"}, "gpt-5.5"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.cfg.ResolvedModel(); got != tc.want {
				t.Fatalf("ResolvedModel = %q, want %q", got, tc.want)
			}
		})
	}
}

// Without an llm_prefilter block the prefilter runs chat on the llm
// provider and model at effort low; every value llm_prefilter sets wins.
func TestResolvedPrefilter(t *testing.T) {
	llm := model.LLMConfig{Provider: "codex", Model: "gpt-5.5", Effort: "high", Timeout: "5m", Concurrency: 2, APIKeys: map[string]string{"openai": "k"}}

	derived := model.BaseConfig{LLM: llm}.ResolvedPrefilter()
	if derived.Protocol != model.PrefilterChat || derived.Provider != "codex" || derived.Model != "gpt-5.5" || derived.Effort != "low" {
		t.Errorf("no block should derive chat on the llm provider and model at low, got %+v", derived)
	}
	if derived.Timeout != "" || derived.Concurrency != 0 || derived.APIKeys["openai"] != "k" {
		t.Errorf("the derived block should keep the prefilter's own bounds and the llm connection, got %+v", derived)
	}

	set := model.BaseConfig{LLM: llm, LLMPrefilter: model.LLMPrefilterConfig{LLMConfig: model.LLMConfig{Model: "gpt-mini", Effort: "minimal"}}}.ResolvedPrefilter()
	if set.Protocol != model.PrefilterChat || set.Provider != "codex" || set.Model != "gpt-mini" || set.Effort != "minimal" {
		t.Errorf("values llm_prefilter sets should win, got %+v", set)
	}

	rate := model.BaseConfig{LLM: llm, LLMPrefilter: model.LLMPrefilterConfig{Protocol: model.PrefilterSystemOne}}.ResolvedPrefilter()
	if rate.Provider != "" || rate.Model != "" || rate.Effort != "" {
		t.Errorf("a rating protocol should take the block as set, got %+v", rate)
	}
}

// Without an llm_prefilter block the deep reading under a Claude host ranks
// with Haiku chat through the claude CLI at effort low, on the llm
// block's credentials; any llm_prefilter value leaves ranking to the
// configured prefilter.
func TestClaudeHostRanking(t *testing.T) {
	llm := model.LLMConfig{Provider: "codex", Model: "gpt-5.5", ClaudeCLI: model.ClaudeCLIConfig{Credentials: model.ClaudeCLICredentialsEnv}}
	ranking, ok := model.BaseConfig{LLM: llm}.ClaudeHostRanking()
	if !ok || ranking.Protocol != model.PrefilterChat || ranking.Provider != "claude-cli" || ranking.Model != "claude-haiku-5-5" || ranking.Effort != "low" {
		t.Fatalf("no block should rank with Haiku chat on the claude CLI at low, got %+v, %v", ranking, ok)
	}
	if ranking.ClaudeCLI.Credentials != model.ClaudeCLICredentialsEnv {
		t.Errorf("the ranking judge should take the llm block's claude CLI credentials, got %q", ranking.ClaudeCLI.Credentials)
	}
	for name, block := range map[string]model.LLMPrefilterConfig{
		"protocol": {Protocol: model.PrefilterChat},
		"model":    {LLMConfig: model.LLMConfig{Model: "haiku"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := (model.BaseConfig{LLM: llm, LLMPrefilter: block}).ClaudeHostRanking(); ok {
				t.Fatal("a configured llm_prefilter should win over the Claude host default")
			}
		})
	}
}

// verify.ranking_timeout bounds the deep reading's ranking pass: 45s by
// default, a set duration wins, and a value that is no duration is ignored.
func TestVerifyConfig_RankingTimeout(t *testing.T) {
	if got := (model.VerifyConfig{}).ResolvedRankingTimeout(); got != model.DefaultRankingTimeout || got.String() != "45s" {
		t.Fatalf("default ranking timeout %s, want 45s", got)
	}
	if got := (model.VerifyConfig{RankingTimeout: "20s"}).ResolvedRankingTimeout(); got.String() != "20s" {
		t.Fatalf("set ranking timeout %s, want 20s", got)
	}
	bad := model.VerifyConfig{RankingTimeout: "soon"}
	if got := bad.ResolvedRankingTimeout(); got != model.DefaultRankingTimeout {
		t.Fatalf("an unreadable ranking timeout %s should leave the default", got)
	}
	if bad.KnownValues().RankingTimeout != "" {
		t.Fatal("KnownValues should clear an unreadable ranking timeout")
	}
	if got := effective(model.EffectiveConfigValues(model.BaseConfig{}, nil, nil))["verify.ranking_timeout"]; got.Value != "45s" || got.Source != model.ConfigSourceDefault {
		t.Fatalf("verify.ranking_timeout = %+v, want the default 45s", got)
	}
}
