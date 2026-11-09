package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

var verifierFiles = map[model.ConfigValueSource]string{
	model.ConfigSourceGlobal:  "global.yaml",
	model.ConfigSourceProject: ".sdd/config.yaml",
	model.ConfigSourceLocal:   ".sdd/config.local.yaml",
}

func verifierLayer(mode model.VerifierMode, skip model.VerifierSkip, enforce *bool) *model.PerRepoConfig {
	cfg := &model.PerRepoConfig{}
	cfg.Verifier = model.VerifierConfig{Mode: mode, Skip: skip}
	cfg.Grounding.Enforce = enforce
	return cfg
}

func boolPtr(v bool) *bool { return &v }

func TestCheckVerifierLayers_ModeRequiresGroundingEnforcement(t *testing.T) {
	err := model.CheckVerifierLayers(model.BaseConfig{}, verifierLayer(model.VerifierLight, "", nil), nil, verifierFiles)
	if err == nil {
		t.Fatal("light without grounding enforcement should be refused")
	}
	for _, want := range []string{"verifier.mode light", "set in .sdd/config.yaml", "grounding.enforce", "unset, off by default"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal should name %q: %v", want, err)
		}
	}

	global := model.BaseConfig{}
	global.Grounding.Enforce = boolPtr(false)
	err = model.CheckVerifierLayers(global, verifierLayer(model.VerifierDeep, "", nil), nil, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), "false in global.yaml") {
		t.Fatalf("the refusal should name the layer that set enforcement off, got %v", err)
	}

	err = model.CheckVerifierLayers(model.BaseConfig{}, verifierLayer(model.VerifierDeep, "", boolPtr(true)), nil, verifierFiles)
	if err != nil {
		t.Fatalf("deep with enforcement on resolves: %v", err)
	}
	err = model.CheckVerifierLayers(model.BaseConfig{}, verifierLayer(model.VerifierOff, "", nil), nil, verifierFiles)
	if err != nil {
		t.Fatalf("off needs no enforcement: %v", err)
	}
}

func TestCheckVerifierLayers_LocalMayOnlyTighten(t *testing.T) {
	committed := verifierLayer(model.VerifierDeep, model.VerifierSkipFailure, boolPtr(true))
	for _, tc := range []struct {
		name  string
		local *model.PerRepoConfig
		want  string
	}{
		{"lower mode", verifierLayer(model.VerifierLight, "", nil), "verifier.mode light is refused in the machine-local config"},
		{"looser skip", verifierLayer("", model.VerifierSkipWeighed, nil), "verifier.skip weighed is refused in the machine-local config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := model.CheckVerifierLayers(model.BaseConfig{}, committed, tc.local, verifierFiles)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), ".sdd/config.local.yaml") || !strings.Contains(err.Error(), "in .sdd/config.yaml") {
				t.Fatalf("want a refusal naming %q and both files, got %v", tc.want, err)
			}
		})
	}

	raised := model.CheckVerifierLayers(model.BaseConfig{}, verifierLayer(model.VerifierLight, model.VerifierSkipFailure, boolPtr(true)), verifierLayer(model.VerifierDeep, model.VerifierSkipNever, nil), verifierFiles)
	if raised != nil {
		t.Fatalf("a local layer raising mode and tightening skip resolves: %v", raised)
	}
}

func TestCheckVerifierLayers_CommittedWinsOverGlobal(t *testing.T) {
	global := model.BaseConfig{}
	global.Verifier.Mode = model.VerifierDeep
	global.Grounding.Enforce = boolPtr(true)
	committed := verifierLayer(model.VerifierLight, "", nil)
	merged := model.MergeConfig(&model.PerRepoConfig{BaseConfig: global}, committed)
	if merged.Verifier.Policy().Mode != model.VerifierLight {
		t.Fatalf("the committed mode should win over the global one, got %s", merged.Verifier.Policy().Mode)
	}
	err := model.CheckVerifierLayers(global, committed, nil, verifierFiles)
	if err != nil {
		t.Fatalf("committed light over global deep resolves: %v", err)
	}
	err = model.CheckVerifierLayers(global, committed, verifierLayer(model.VerifierLight, "", nil), verifierFiles)
	if err != nil {
		t.Fatalf("a local value equal to the committed one resolves: %v", err)
	}
}

func TestCheckVerifierLayers_RefusesUnknownValues(t *testing.T) {
	err := model.CheckVerifierLayers(model.BaseConfig{}, verifierLayer("strict", "", boolPtr(true)), nil, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `.sdd/config.yaml: verifier.mode "strict"`) {
		t.Fatalf("an unknown mode should be refused naming file and key, got %v", err)
	}
	err = model.CheckVerifierLayers(model.BaseConfig{}, nil, verifierLayer("", "sometimes", nil), verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `.sdd/config.local.yaml: verifier.skip "sometimes"`) {
		t.Fatalf("an unknown skip should be refused naming file and key, got %v", err)
	}
}

func TestVerifierPolicyDefaults(t *testing.T) {
	policy := model.VerifierConfig{}.Policy()
	if policy.Mode != model.VerifierOff || policy.Skip != model.VerifierSkipFailure {
		t.Fatalf("defaults are off and failure, got %+v", policy)
	}
}

func TestStrictnessSetsBothKeysAsAValidPair(t *testing.T) {
	for _, s := range model.Strictnesses() {
		enforce, mode := s.Settings()
		cfg := &model.PerRepoConfig{}
		cfg.Grounding.Enforce = &enforce
		cfg.Verifier.Mode = mode
		err := model.CheckVerifierLayers(model.BaseConfig{}, cfg, nil, verifierFiles)
		if err != nil {
			t.Errorf("strictness %s should set a pair that resolves: %v", s, err)
		}
	}
	enforce, mode := model.StrictnessGrounding.Settings()
	if !enforce || mode != model.VerifierOff {
		t.Fatalf("grounding enforces without a verifier, got %v %s", enforce, mode)
	}
	_, err := model.ParseStrictness("strict")
	if err == nil {
		t.Fatal("an answer outside the set should be refused")
	}
}

func TestCheckVerifierLayers_LaunchIsPersonal(t *testing.T) {
	committed := verifierLayer(model.VerifierDeep, "", boolPtr(true))
	committed.Verifier.Launch = model.VerifierLaunchClaude
	local := &model.PerRepoConfig{}
	local.Verifier.Launch = model.VerifierLaunchSubagent
	err := model.CheckVerifierLayers(model.BaseConfig{}, committed, local, verifierFiles)
	if err != nil {
		t.Fatalf("a machine-local launch may differ from the committed one: %v", err)
	}
	merged := model.MergeConfig(committed, local)
	if merged.Verifier.Policy().Launch != model.VerifierLaunchSubagent {
		t.Fatalf("the machine-local launch should win, got %s", merged.Verifier.Policy().Launch)
	}
	if (model.VerifierConfig{}).Policy().Launch != model.VerifierLaunchSubagent {
		t.Fatal("an unset launch should be the subagent")
	}
	local.Verifier.Launch = "remote"
	err = model.CheckVerifierLayers(model.BaseConfig{}, committed, local, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `.sdd/config.local.yaml: verifier.launch "remote"`) {
		t.Fatalf("an unknown launch should be refused naming file and key, got %v", err)
	}
}

func TestCheckVerifierLayers_DriverStaysOffTheCommittedConfig(t *testing.T) {
	committed := verifierLayer(model.VerifierDeep, "", boolPtr(true))
	committed.Verifier.Driver = model.VerifierDriver{Command: "/opt/driver"}
	err := model.CheckVerifierLayers(model.BaseConfig{}, committed, nil, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), ".sdd/config.yaml: verifier.driver is refused in the committed config") {
		t.Fatalf("a committed driver should be refused naming the key, got %v", err)
	}

	committed.Verifier.Driver = model.VerifierDriver{}
	committed.Verifier.Launch = model.VerifierLaunchCustomDriver
	err = model.CheckVerifierLayers(model.BaseConfig{}, committed, nil, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), "verifier.launch custom_driver needs verifier.driver.command") {
		t.Fatalf("a custom_driver launch without a driver should be refused naming the key, got %v", err)
	}

	global := model.BaseConfig{}
	global.Verifier.Driver = model.VerifierDriver{Command: "/opt/driver", Args: []string{"--round"}}
	err = model.CheckVerifierLayers(global, committed, nil, verifierFiles)
	if err != nil {
		t.Fatalf("a driver from the user-global config serves a committed custom_driver launch: %v", err)
	}
	merged := model.MergeConfig(&model.PerRepoConfig{BaseConfig: global}, committed)
	if driver := merged.Verifier.Policy().Driver; driver.Command != "/opt/driver" || len(driver.Args) != 1 {
		t.Fatalf("the resolved policy should carry the driver, got %+v", driver)
	}
}

func TestVerifierLaunchChoices_OfferOnlyWhatTheProjectRuns(t *testing.T) {
	cases := map[string]struct {
		agents []model.AgentTarget
		driver model.VerifierDriver
		want   string
	}{
		"subagent first, agents in order": {agents: []model.AgentTarget{model.AgentCodex, model.AgentClaude}, want: "subagent codex claude"},
		"only named agents":               {agents: []model.AgentTarget{model.AgentClaude}, want: "subagent claude"},
		"no headless cli":                 {agents: []model.AgentTarget{"gemini", model.AgentClaude}, want: "subagent claude"},
		"driver once configured":          {agents: []model.AgentTarget{model.AgentClaude}, driver: model.VerifierDriver{Command: "/usr/local/bin/verify-driver"}, want: "subagent claude custom_driver"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var names []string
			for _, choice := range model.VerifierLaunchChoices(tc.agents, tc.driver) {
				names = append(names, string(choice))
			}
			if got := strings.Join(names, " "); got != tc.want {
				t.Fatalf("init should offer the launches the project runs, in order, got %q", got)
			}
		})
	}
}

func TestVerifierLaunchAsked_OnlyUnderDeepWhileUnset(t *testing.T) {
	cases := map[string]struct {
		mode     model.VerifierMode
		recorded model.VerifierConfig
		want     bool
	}{
		"deep, unset":    {mode: model.VerifierDeep, want: true},
		"deep, set":      {mode: model.VerifierDeep, recorded: model.VerifierConfig{Launch: model.VerifierLaunchSubagent}},
		"light":          {mode: model.VerifierLight},
		"no mode at all": {},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := model.VerifierLaunchAsked(tc.mode, tc.recorded); got != tc.want {
				t.Fatalf("init should ask for the launch only under deep while no layer sets one, got %v", got)
			}
		})
	}
}

// Each launch's model and effort describe the machine like the launch: the
// machine-local value wins field by field, and the blocks stay apart.
func TestVerifierAgentsLayerPerLaunch(t *testing.T) {
	committed := verifierLayer(model.VerifierDeep, "", boolPtr(true))
	committed.Verifier.Claude = model.VerifierAgent{Model: "sonnet", Effort: "high"}
	committed.Verifier.Codex = model.VerifierAgent{Model: "gpt-5.5"}
	local := &model.PerRepoConfig{}
	local.Verifier.Claude = model.VerifierAgent{Effort: "max"}
	local.Verifier.Subagent = model.VerifierAgent{Model: "opus"}
	err := model.CheckVerifierLayers(model.BaseConfig{}, committed, local, verifierFiles)
	if err != nil {
		t.Fatalf("machine-local model and effort may differ from the committed ones: %v", err)
	}

	policy := model.MergeConfig(committed, local).Verifier.Policy()

	if policy.Claude != (model.VerifierAgent{Model: "sonnet", Effort: "max"}) {
		t.Errorf("the machine-local effort should win and the committed model stay, got %+v", policy.Claude)
	}
	if policy.Codex != (model.VerifierAgent{Model: "gpt-5.5"}) || policy.Subagent != (model.VerifierAgent{Model: "opus"}) {
		t.Errorf("each launch should keep its own block, got codex %+v and subagent %+v", policy.Codex, policy.Subagent)
	}
}

// Only claude's effort is a closed set; codex and the subagent tool accept
// model-defined levels.
func TestCheckVerifierLayers_RefusesAnUnknownClaudeEffort(t *testing.T) {
	local := &model.PerRepoConfig{}
	local.Verifier.Claude.Effort = "ultra"
	err := model.CheckVerifierLayers(model.BaseConfig{}, nil, local, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `.sdd/config.local.yaml: verifier.claude.effort "ultra"`) {
		t.Fatalf("an effort the claude CLI does not take should be refused naming file and key, got %v", err)
	}
	local.Verifier.Claude.Effort = ""
	local.Verifier.Codex.Effort = "ultra"
	local.Verifier.Subagent.Effort = "ultra"
	err = model.CheckVerifierLayers(model.BaseConfig{}, nil, local, verifierFiles)
	if err != nil {
		t.Fatalf("a model-defined effort should pass for codex and the subagent: %v", err)
	}
}
