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

func verifyLayer(mode model.VerifyMode, skip model.VerifierSkip) *model.PerRepoConfig {
	cfg := &model.PerRepoConfig{}
	cfg.Verify = model.VerifyConfig{Mode: mode, Skip: skip}
	return cfg
}

// Each mode sets grounding enforcement and the verifier mode as one pair, and
// an unset mode is prose.
func TestVerifyModeSetsEnforcementAndVerifier(t *testing.T) {
	cases := []struct {
		mode     model.VerifyMode
		enforce  bool
		verifier model.VerifierMode
	}{
		{"", true, model.VerifierProse},
		{model.VerifyOff, false, model.VerifierOff},
		{model.VerifyGrounding, true, model.VerifierOff},
		{model.VerifyProse, true, model.VerifierProse},
		{model.VerifyDeep, true, model.VerifierDeep},
	}
	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			block := model.VerifyConfig{Mode: tc.mode}
			if got := block.GroundingPolicy().Enforce; got != tc.enforce {
				t.Errorf("grounding enforcement should follow the mode, got %v", got)
			}
			if got := block.VerifierPolicy().Mode; got != tc.verifier {
				t.Errorf("the verifier mode should follow the mode, got %s", got)
			}
			err := model.CheckVerifyLayers(model.BaseConfig{}, verifyLayer(tc.mode, ""), nil, verifierFiles)
			if err != nil {
				t.Errorf("every mode should resolve: %v", err)
			}
		})
	}
}

func TestParseVerifyMode(t *testing.T) {
	for _, mode := range []string{"off", "grounding", "prose", "deep"} {
		parsed, err := model.ParseVerifyMode(mode)
		if err != nil || string(parsed) != mode {
			t.Errorf("mode %s should parse, got %q, %v", mode, parsed, err)
		}
	}
	_, err := model.ParseVerifyMode("light")
	if err == nil || !strings.Contains(err.Error(), `verify.mode "light"`) {
		t.Fatalf("a mode outside the set should be refused naming the key, got %v", err)
	}
}

func TestCheckVerifyLayers_LocalMayOnlyTighten(t *testing.T) {
	committed := verifyLayer(model.VerifyProse, model.VerifierSkipFailure)
	for _, tc := range []struct {
		name  string
		local *model.PerRepoConfig
		want  string
	}{
		{"lower mode", verifyLayer(model.VerifyGrounding, ""), "verify.mode grounding is refused in the machine-local config"},
		{"mode off", verifyLayer(model.VerifyOff, ""), "verify.mode off is refused in the machine-local config"},
		{"looser skip", verifyLayer("", model.VerifierSkipWeighed), "verify.skip weighed is refused in the machine-local config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := model.CheckVerifyLayers(model.BaseConfig{}, committed, tc.local, verifierFiles)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), ".sdd/config.local.yaml") || !strings.Contains(err.Error(), "in .sdd/config.yaml") {
				t.Fatalf("want a refusal naming %q and both files, got %v", tc.want, err)
			}
		})
	}

	unsetShared := model.CheckVerifyLayers(model.BaseConfig{}, nil, verifyLayer(model.VerifyGrounding, ""), verifierFiles)
	if unsetShared == nil || !strings.Contains(unsetShared.Error(), "may only raise the prose the shared layers resolve to") {
		t.Fatalf("with no shared mode the default prose should bound the local layer, got %v", unsetShared)
	}

	raised := model.CheckVerifyLayers(model.BaseConfig{}, committed, verifyLayer(model.VerifyDeep, model.VerifierSkipNever), verifierFiles)
	if raised != nil {
		t.Fatalf("a local layer raising mode and tightening skip resolves: %v", raised)
	}
}

func TestCheckVerifyLayers_CommittedWinsOverGlobal(t *testing.T) {
	global := model.BaseConfig{}
	global.Verify.Mode = model.VerifyDeep
	committed := verifyLayer(model.VerifyProse, "")
	merged := model.MergeConfig(&model.PerRepoConfig{BaseConfig: global}, committed)
	if merged.Verify.VerifierPolicy().Mode != model.VerifierProse {
		t.Fatalf("the committed mode should win over the global one, got %s", merged.Verify.VerifierPolicy().Mode)
	}
	err := model.CheckVerifyLayers(global, committed, nil, verifierFiles)
	if err != nil {
		t.Fatalf("committed prose over global deep resolves: %v", err)
	}
	err = model.CheckVerifyLayers(global, committed, verifyLayer(model.VerifyProse, ""), verifierFiles)
	if err != nil {
		t.Fatalf("a local value equal to the committed one resolves: %v", err)
	}
}

func TestCheckVerifyLayers_RefusesUnknownValues(t *testing.T) {
	err := model.CheckVerifyLayers(model.BaseConfig{}, verifyLayer("strict", ""), nil, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `.sdd/config.yaml: verify.mode "strict"`) {
		t.Fatalf("an unknown mode should be refused naming file and key, got %v", err)
	}
	err = model.CheckVerifyLayers(model.BaseConfig{}, nil, verifyLayer("", "sometimes"), verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `.sdd/config.local.yaml: verify.skip "sometimes"`) {
		t.Fatalf("an unknown skip should be refused naming file and key, got %v", err)
	}
}

func TestVerifierPolicyDefaults(t *testing.T) {
	policy := model.VerifyConfig{}.VerifierPolicy()
	if policy.Mode != model.VerifierProse || policy.Skip != model.VerifierSkipFailure {
		t.Fatalf("defaults are prose and failure, got %+v", policy)
	}
}

func TestCheckVerifyLayers_RunnerIsPersonal(t *testing.T) {
	committed := verifyLayer(model.VerifyDeep, "")
	committed.Verify.Runner = model.VerifierRunnerClaude
	local := &model.PerRepoConfig{}
	local.Verify.Runner = model.VerifierRunnerSubagent
	err := model.CheckVerifyLayers(model.BaseConfig{}, committed, local, verifierFiles)
	if err != nil {
		t.Fatalf("a machine-local runner may differ from the committed one: %v", err)
	}
	merged := model.MergeConfig(committed, local)
	if merged.Verify.VerifierPolicy().Runner != model.VerifierRunnerSubagent {
		t.Fatalf("the machine-local runner should win, got %s", merged.Verify.VerifierPolicy().Runner)
	}
	if (model.VerifyConfig{}).VerifierPolicy().Runner != model.VerifierRunnerSubagent {
		t.Fatal("an unset runner should be the subagent")
	}
	local.Verify.Runner = "remote"
	err = model.CheckVerifyLayers(model.BaseConfig{}, committed, local, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `.sdd/config.local.yaml: verify.runner "remote"`) {
		t.Fatalf("an unknown runner should be refused naming file and key, got %v", err)
	}
}

func TestCheckVerifyLayers_DriverStaysOffTheCommittedConfig(t *testing.T) {
	committed := verifyLayer(model.VerifyDeep, "")
	committed.Verify.Driver = model.VerifierDriver{Command: "/opt/driver"}
	err := model.CheckVerifyLayers(model.BaseConfig{}, committed, nil, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), ".sdd/config.yaml: verify.driver is refused in the committed config") {
		t.Fatalf("a committed driver should be refused naming the key, got %v", err)
	}

	committed.Verify.Driver = model.VerifierDriver{}
	committed.Verify.Runner = model.VerifierRunnerDriver
	err = model.CheckVerifyLayers(model.BaseConfig{}, committed, nil, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), "verify.runner driver needs verify.driver.command") {
		t.Fatalf("a driver runner without a command should be refused naming the key, got %v", err)
	}

	global := model.BaseConfig{}
	global.Verify.Driver = model.VerifierDriver{Command: "/opt/driver", Args: []string{"--round"}}
	err = model.CheckVerifyLayers(global, committed, nil, verifierFiles)
	if err != nil {
		t.Fatalf("a driver from the user-global config serves a committed driver runner: %v", err)
	}
	merged := model.MergeConfig(&model.PerRepoConfig{BaseConfig: global}, committed)
	if driver := merged.Verify.VerifierPolicy().Driver; driver.Command != "/opt/driver" || len(driver.Args) != 1 {
		t.Fatalf("the resolved policy should carry the driver, got %+v", driver)
	}
}

func TestVerifierRunnerChoices_OfferOnlyWhatTheProjectRuns(t *testing.T) {
	cases := map[string]struct {
		agents []model.AgentTarget
		driver model.VerifierDriver
		want   string
	}{
		"subagent first, agents in order": {agents: []model.AgentTarget{model.AgentCodex, model.AgentClaude}, want: "subagent codex claude"},
		"only named agents":               {agents: []model.AgentTarget{model.AgentClaude}, want: "subagent claude"},
		"no headless cli":                 {agents: []model.AgentTarget{"gemini", model.AgentClaude}, want: "subagent claude"},
		"driver once configured":          {agents: []model.AgentTarget{model.AgentClaude}, driver: model.VerifierDriver{Command: "/usr/local/bin/verify-driver"}, want: "subagent claude driver"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var names []string
			for _, choice := range model.VerifierRunnerChoices(tc.agents, tc.driver) {
				names = append(names, string(choice))
			}
			if got := strings.Join(names, " "); got != tc.want {
				t.Fatalf("init should offer the runners the project runs, in order, got %q", got)
			}
		})
	}
}

func TestVerifierRunnerAsked_OnlyUnderDeepWhileUnset(t *testing.T) {
	cases := map[string]struct {
		mode     model.VerifyMode
		recorded model.VerifyConfig
		want     bool
	}{
		"deep, unset":    {mode: model.VerifyDeep, want: true},
		"deep, set":      {mode: model.VerifyDeep, recorded: model.VerifyConfig{Runner: model.VerifierRunnerSubagent}},
		"prose":          {mode: model.VerifyProse},
		"no mode at all": {},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := model.VerifierRunnerAsked(tc.mode, tc.recorded); got != tc.want {
				t.Fatalf("init should ask for the runner only under deep while no layer sets one, got %v", got)
			}
		})
	}
}

// Each agent family's model and effort describe the machine like the runner:
// the machine-local value wins field by field, the blocks stay apart, and an
// unset field takes the family's default.
func TestVerifierAgentsLayerPerFamily(t *testing.T) {
	committed := verifyLayer(model.VerifyDeep, "")
	committed.Verify.Claude = model.ClaudeAgent{Model: "opus", Effort: "high"}
	committed.Verify.Codex = model.VerifierAgent{Model: "gpt-5.5"}
	local := &model.PerRepoConfig{}
	local.Verify.Claude = model.ClaudeAgent{Effort: "max"}
	err := model.CheckVerifyLayers(model.BaseConfig{}, committed, local, verifierFiles)
	if err != nil {
		t.Fatalf("machine-local model and effort may differ from the committed ones: %v", err)
	}

	policy := model.MergeConfig(committed, local).Verify.VerifierPolicy()

	if policy.Claude != (model.VerifierAgent{Model: "opus", Effort: "max"}) {
		t.Errorf("the machine-local effort should win and the committed model stay, got %+v", policy.Claude)
	}
	if policy.Codex != (model.VerifierAgent{Model: "gpt-5.5", Effort: "medium"}) {
		t.Errorf("codex should keep its own model and take the default effort, got %+v", policy.Codex)
	}
}

func TestVerifierAgentDefaults(t *testing.T) {
	policy := model.VerifyConfig{}.VerifierPolicy()
	if policy.Claude != (model.VerifierAgent{Model: "sonnet", Effort: "medium"}) {
		t.Errorf("an unset verify.claude should resolve to sonnet at medium, got %+v", policy.Claude)
	}
	if policy.Codex != (model.VerifierAgent{Effort: "medium"}) {
		t.Errorf("an unset verify.codex should leave codex's model at medium, got %+v", policy.Codex)
	}
}

// Claude Code reports the client name claude-code and Codex codex-mcp-client;
// the match is a case-insensitive substring so a renamed build still matches.
func TestSubagentAgent_MatchesTheClientNameCaseInsensitively(t *testing.T) {
	policy := model.VerifierPolicy{Claude: model.VerifierAgent{Model: "opus"}, Codex: model.VerifierAgent{Model: "gpt-5.5"}}
	for client, want := range map[string]model.VerifierAgent{
		"claude-code":      {Model: "opus"},
		"Claude Desktop":   {Model: "opus"},
		"codex-mcp-client": {Model: "gpt-5.5"},
		"CODEX":            {Model: "gpt-5.5"},
		"cursor":           {},
		"":                 {},
	} {
		t.Run(client, func(t *testing.T) {
			if got := policy.SubagentAgent(client); got != want {
				t.Fatalf("client %q should take %+v, got %+v", client, want, got)
			}
		})
	}
}

// Only claude's effort is a closed set; codex accepts model-defined levels.
func TestCheckVerifyLayers_RefusesAnUnknownClaudeEffort(t *testing.T) {
	local := &model.PerRepoConfig{}
	local.Verify.Claude.Effort = "ultra"
	err := model.CheckVerifyLayers(model.BaseConfig{}, nil, local, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `.sdd/config.local.yaml: verify.claude.effort "ultra"`) {
		t.Fatalf("an effort the claude CLI does not take should be refused naming file and key, got %v", err)
	}
	local.Verify.Claude.Effort = ""
	local.Verify.Codex.Effort = "ultra"
	err = model.CheckVerifyLayers(model.BaseConfig{}, nil, local, verifierFiles)
	if err != nil {
		t.Fatalf("a model-defined effort should pass for codex: %v", err)
	}
}
