package model_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

var verifierFiles = map[model.ConfigValueSource]string{
	model.ConfigSourceGlobal:  "global.yaml",
	model.ConfigSourceProject: ".sdd/config.yaml",
	model.ConfigSourceLocal:   ".sdd/config.local.yaml",
}

// checkVerifyLayers is the first value HonoredVerifyLayers drops, as an
// error; nil when it drops none.
func checkVerifyLayers(global model.BaseConfig, committed, local *model.PerRepoConfig, files map[model.ConfigValueSource]string) error {
	_, _, _, dropped := model.HonoredVerifyLayers(global, committed, local, files)
	if len(dropped) == 0 {
		return nil
	}
	return errors.New(dropped[0].Reason)
}

// A read drops what a layer may not set where it stands and resolves the
// rest: the shared value stands in for a loosening local one.
func TestHonoredVerifyLayers_KeepsTheSharedValueForADroppedLocalOne(t *testing.T) {
	committed, local := verifyLayer(model.VerifyDeep, ""), verifyLayer(model.VerifyProse, "")
	_, honoredCommitted, honoredLocal, dropped := model.HonoredVerifyLayers(model.BaseConfig{}, committed, local, verifierFiles)
	if len(dropped) != 1 || dropped[0].Key != "verify.mode" || dropped[0].Source != model.ConfigSourceLocal {
		t.Fatalf("the loosening local verify.mode should be dropped from the local layer, got %+v", dropped)
	}
	resolved := model.MergeConfig(honoredCommitted, honoredLocal)
	if resolved.Verify.Mode != model.VerifyDeep || local.Verify.Mode != model.VerifyProse {
		t.Fatalf("the committed mode should resolve and the caller's layer stay untouched, got %q and %q", resolved.Verify.Mode, local.Verify.Mode)
	}
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
			err := checkVerifyLayers(model.BaseConfig{}, verifyLayer(tc.mode, ""), nil, verifierFiles)
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

func TestHonoredVerifyLayers_LocalMayOnlyTighten(t *testing.T) {
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
			err := checkVerifyLayers(model.BaseConfig{}, committed, tc.local, verifierFiles)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), ".sdd/config.local.yaml") || !strings.Contains(err.Error(), "in .sdd/config.yaml") {
				t.Fatalf("want a refusal naming %q and both files, got %v", tc.want, err)
			}
		})
	}

	unsetShared := checkVerifyLayers(model.BaseConfig{}, nil, verifyLayer(model.VerifyGrounding, ""), verifierFiles)
	if unsetShared == nil || !strings.Contains(unsetShared.Error(), "may only raise the prose the shared layers resolve to") {
		t.Fatalf("with no shared mode the default prose should bound the local layer, got %v", unsetShared)
	}

	raised := checkVerifyLayers(model.BaseConfig{}, committed, verifyLayer(model.VerifyDeep, model.VerifierSkipNever), verifierFiles)
	if raised != nil {
		t.Fatalf("a local layer raising mode and tightening skip resolves: %v", raised)
	}
}

func TestHonoredVerifyLayers_CommittedWinsOverGlobal(t *testing.T) {
	global := model.BaseConfig{}
	global.Verify.Mode = model.VerifyDeep
	committed := verifyLayer(model.VerifyProse, "")
	merged := model.MergeConfig(&model.PerRepoConfig{BaseConfig: global}, committed)
	if merged.Verify.VerifierPolicy().Mode != model.VerifierProse {
		t.Fatalf("the committed mode should win over the global one, got %s", merged.Verify.VerifierPolicy().Mode)
	}
	err := checkVerifyLayers(global, committed, nil, verifierFiles)
	if err != nil {
		t.Fatalf("committed prose over global deep resolves: %v", err)
	}
	err = checkVerifyLayers(global, committed, verifyLayer(model.VerifyProse, ""), verifierFiles)
	if err != nil {
		t.Fatalf("a local value equal to the committed one resolves: %v", err)
	}
}

func TestHonoredVerifyLayers_PassesUnknownValues(t *testing.T) {
	local := verifyLayer("deeep", "sometimes")
	local.Verify.Runner = "robot"
	local.Verify.Claude.Effort = "ultra"
	var global model.BaseConfig
	global.Verify.Mode = "strict"
	for _, tc := range []struct {
		name             string
		global           model.BaseConfig
		committed, local *model.PerRepoConfig
	}{
		{"global", global, nil, nil},
		{"committed", model.BaseConfig{}, verifyLayer("strict", ""), nil},
		{"local under a deep committed mode", model.BaseConfig{}, verifyLayer(model.VerifyDeep, model.VerifierSkipNever), local},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkVerifyLayers(tc.global, tc.committed, tc.local, verifierFiles)
			if err != nil {
				t.Fatalf("a read should pass a value it does not know: %v", err)
			}
		})
	}
}

func TestVerifyKeyRefusal_RefusesAnUnknownValueOnWrite(t *testing.T) {
	err := model.VerifyKeyRefusal("verify.mode", model.BaseConfig{}, verifyLayer("strict", ""), nil, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `.sdd/config.yaml: verify.mode "strict"`) {
		t.Fatalf("a write should refuse an unknown mode naming file and key, got %v", err)
	}
	local := &model.PerRepoConfig{}
	local.Verify.Claude.Effort = "ultra"
	err = model.VerifyKeyRefusal("verify.claude.effort", model.BaseConfig{}, nil, local, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `.sdd/config.local.yaml: verify.claude.effort "ultra"`) {
		t.Fatalf("a write should refuse an effort the claude CLI does not take, got %v", err)
	}
}

func TestMergeBaseConfig_AnUnknownVerifyValueLeavesTheLayerBelow(t *testing.T) {
	var below, above model.BaseConfig
	below.Verify = model.VerifyConfig{Mode: model.VerifyDeep, Skip: model.VerifierSkipNever, Runner: model.VerifierRunnerClaude}
	below.Verify.Claude.Effort = "high"
	above.Verify = model.VerifyConfig{Mode: "deeep", Skip: "sometimes", Runner: "robot"}
	above.Verify.Claude.Effort = "ultra"
	policy := model.MergeBaseConfig(below, above).Verify.VerifierPolicy()
	if got := model.MergeBaseConfig(below, above).Verify.ResolvedMode(); got != model.VerifyDeep {
		t.Errorf("an unknown mode should leave the mode below, got %q", got)
	}
	if policy.Skip != model.VerifierSkipNever || policy.Runner != model.VerifierRunnerClaude || policy.Claude.Effort != "high" {
		t.Errorf("unknown skip, runner and effort should leave the values below, got %+v", policy)
	}
	alone := model.MergeBaseConfig(above, model.BaseConfig{}).Verify
	if alone.ResolvedMode() != model.DefaultVerifyMode || alone.Skip != "" || alone.Runner != "" || alone.Claude.Effort != "" {
		t.Errorf("unknown values with no layer below should clear to unset, got %+v", alone)
	}
}

func TestVerifyConfig_UnknownValuesResolveAsUnset(t *testing.T) {
	block := model.VerifyConfig{Mode: "deeep", Skip: "sometimes", Runner: "robot"}
	block.Claude.Effort = "ultra"
	if got := block.ResolvedMode(); got != model.DefaultVerifyMode {
		t.Errorf("an unknown mode should resolve to the default, got %q", got)
	}
	if got, want := block.VerifierPolicy(), (model.VerifyConfig{}).VerifierPolicy(); got.Skip != want.Skip || got.Runner != want.Runner || got.Claude != want.Claude {
		t.Errorf("unknown policy values should resolve to the defaults, got %+v", got)
	}
}

func TestIgnoredSettings_ReportsUnknownVerifyValues(t *testing.T) {
	var layer model.BaseConfig
	layer.Verify = model.VerifyConfig{Mode: "deeep", Skip: "sometimes", Runner: "robot"}
	layer.Verify.Claude.Effort = "ultra"
	reasons := map[string]string{}
	for _, s := range model.IgnoredSettings(layer, true) {
		reasons[s.Key] = s.Reason
	}
	for key, value := range map[string]string{"verify.mode": "deeep", "verify.skip": "sometimes", "verify.runner": "robot", "verify.claude.effort": "ultra"} {
		if !strings.Contains(reasons[key], `"`+value+`" is not`) {
			t.Errorf("%s should be reported with its unknown value, got %q", key, reasons[key])
		}
	}
}

func TestVerifierPolicyDefaults(t *testing.T) {
	policy := model.VerifyConfig{}.VerifierPolicy()
	if policy.Mode != model.VerifierProse || policy.Skip != model.VerifierSkipFailure {
		t.Fatalf("defaults are prose and failure, got %+v", policy)
	}
}

func TestHonoredVerifyLayers_RunnerIsPersonal(t *testing.T) {
	committed := verifyLayer(model.VerifyDeep, "")
	committed.Verify.Runner = model.VerifierRunnerClaude
	local := &model.PerRepoConfig{}
	local.Verify.Runner = model.VerifierRunnerSubagent
	err := checkVerifyLayers(model.BaseConfig{}, committed, local, verifierFiles)
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
	err = model.VerifyKeyRefusal("verify.runner", model.BaseConfig{}, committed, local, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `.sdd/config.local.yaml: verify.runner "remote"`) {
		t.Fatalf("a write should refuse an unknown runner naming file and key, got %v", err)
	}
}

func TestHonoredVerifyLayers_DriverStaysOffTheCommittedConfig(t *testing.T) {
	committed := verifyLayer(model.VerifyDeep, "")
	committed.Verify.Driver = model.VerifierDriver{Command: "/opt/driver"}
	err := checkVerifyLayers(model.BaseConfig{}, committed, nil, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), ".sdd/config.yaml: verify.driver is refused in the committed config") {
		t.Fatalf("a committed driver should be refused naming the key, got %v", err)
	}

	committed.Verify.Driver = model.VerifierDriver{}
	committed.Verify.Runner = model.VerifierRunnerDriver
	err = checkVerifyLayers(model.BaseConfig{}, committed, nil, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), "verify.runner driver needs verify.driver.command") {
		t.Fatalf("a driver runner without a command should be refused naming the key, got %v", err)
	}

	global := model.BaseConfig{}
	global.Verify.Driver = model.VerifierDriver{Command: "/opt/driver", Args: []string{"--round"}}
	err = checkVerifyLayers(global, committed, nil, verifierFiles)
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
		"subagent first, no codex runner": {agents: []model.AgentTarget{model.AgentCodex, model.AgentClaude}, want: "subagent claude"},
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

// The verifier's model and effort describe the machine like the runner: the
// machine-local value wins field by field.
func TestVerifierAgentLayersPerField(t *testing.T) {
	committed := verifyLayer(model.VerifyDeep, "")
	committed.Verify.Claude = model.ClaudeAgent{Model: "opus", Effort: "high"}
	local := &model.PerRepoConfig{}
	local.Verify.Claude = model.ClaudeAgent{Effort: "max"}
	err := checkVerifyLayers(model.BaseConfig{}, committed, local, verifierFiles)
	if err != nil {
		t.Fatalf("machine-local model and effort may differ from the committed ones: %v", err)
	}

	policy := model.MergeConfig(committed, local).Verify.VerifierPolicy()

	if policy.Claude != (model.VerifierAgent{Model: "opus", Effort: "max"}) {
		t.Errorf("the machine-local effort should win and the committed model stay, got %+v", policy.Claude)
	}
}

func TestVerifierAgentDefaults(t *testing.T) {
	policy := model.VerifyConfig{}.VerifierPolicy()
	if policy.Claude != (model.VerifierAgent{Model: "sonnet", Effort: "medium"}) {
		t.Errorf("an unset verify.claude should resolve to sonnet at medium, got %+v", policy.Claude)
	}
}

// Claude Code reports the client name claude-code; the match is a
// case-insensitive substring so a renamed build still matches.
func TestSubagentAgent_MatchesTheClientNameCaseInsensitively(t *testing.T) {
	policy := model.VerifierPolicy{Claude: model.VerifierAgent{Model: "opus"}}
	for client, want := range map[string]model.VerifierAgent{
		"claude-code":      {Model: "opus"},
		"Claude Desktop":   {Model: "opus"},
		"codex-mcp-client": {},
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

func TestVerifyIdleTimeout(t *testing.T) {
	if got := (model.VerifyConfig{}).VerifierPolicy().IdleTimeout; got != model.DefaultVerifierIdleTimeout {
		t.Fatalf("an unset idle_timeout should take the default, got %s", got)
	}
	committed := verifyLayer(model.VerifyDeep, "")
	committed.Verify.IdleTimeout = "20m"
	local := &model.PerRepoConfig{}
	local.Verify.IdleTimeout = "5m"
	if got := model.MergeConfig(committed, local).Verify.VerifierPolicy().IdleTimeout; got != 5*time.Minute {
		t.Fatalf("the machine-local idle_timeout should win, got %s", got)
	}
	local.Verify.IdleTimeout = "soon"
	err := model.VerifyKeyRefusal("verify.idle_timeout", model.BaseConfig{}, committed, local, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `.sdd/config.local.yaml: verify.idle_timeout "soon" is no positive duration`) {
		t.Fatalf("a write should refuse an idle_timeout that is no duration naming file and key, got %v", err)
	}
}

func TestVerifyRoundTimeout(t *testing.T) {
	if got := (model.VerifyConfig{}).VerifierPolicy().RoundTimeout; got != model.DefaultVerifierRoundTimeout {
		t.Fatalf("an unset round_timeout should take the default, got %s", got)
	}
	committed := verifyLayer(model.VerifyDeep, "")
	committed.Verify.RoundTimeout = "20m"
	local := &model.PerRepoConfig{}
	local.Verify.RoundTimeout = "5m"
	if err := checkVerifyLayers(model.BaseConfig{}, committed, local, verifierFiles); err != nil {
		t.Fatalf("a machine-local round_timeout may differ from the committed one: %v", err)
	}
	if got := model.MergeConfig(committed, local).Verify.VerifierPolicy().RoundTimeout; got != 5*time.Minute {
		t.Fatalf("the machine-local round_timeout should win, got %s", got)
	}
	local.Verify.RoundTimeout = "soon"
	if err := checkVerifyLayers(model.BaseConfig{}, committed, local, verifierFiles); err != nil {
		t.Fatalf("a read should pass a round_timeout that is no duration: %v", err)
	}
	if got := model.MergeConfig(committed, local).Verify.VerifierPolicy().RoundTimeout; got != 20*time.Minute {
		t.Fatalf("a round_timeout that is no duration should leave the layer below, got %s", got)
	}
	err := model.VerifyKeyRefusal("verify.round_timeout", model.BaseConfig{}, committed, local, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `.sdd/config.local.yaml: verify.round_timeout "soon" is no positive duration`) {
		t.Fatalf("a write should refuse a round_timeout that is no duration naming file and key, got %v", err)
	}
}
