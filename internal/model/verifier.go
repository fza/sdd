package model

import (
	"fmt"
	"slices"
	"strings"
)

// VerifyMode is how carefully a capture is checked against the graph before
// it is written (20261109-004729-d-cpt-xs5, V-D28): it sets grounding
// enforcement and the draft verifier together, so no invalid pair can be
// chosen. The modes build up, so their order is the strictness a
// machine-local layer may only raise.
type VerifyMode string

const (
	VerifyOff       VerifyMode = "off"
	VerifyGrounding VerifyMode = "grounding"
	VerifyProse     VerifyMode = "prose"
	VerifyDeep      VerifyMode = "deep"
)

// DefaultVerifyMode is the mode an unset verify.mode resolves to
// (20261109-222220-d-tac-4ev).
const DefaultVerifyMode = VerifyProse

// AllowedValues lists the modes, loosest first.
func (VerifyMode) AllowedValues() []string {
	return closedValues(VerifyOff, VerifyGrounding, VerifyProse, VerifyDeep)
}

// ParseVerifyMode reads a mode, refusing one outside the set.
func ParseVerifyMode(value string) (VerifyMode, error) {
	err := checkClosedValue("verify.mode", value, VerifyMode("").AllowedValues())
	if err != nil {
		return "", err
	}
	return VerifyMode(value), nil
}

// Settings returns whether the mode enforces grounding and the verifier mode
// it runs; an empty mode is off.
func (m VerifyMode) Settings() (enforce bool, verifier VerifierMode) {
	switch m {
	case VerifyGrounding:
		return true, VerifierOff
	case VerifyProse:
		return true, VerifierProse
	case VerifyDeep:
		return true, VerifierDeep
	}
	return false, VerifierOff
}

// VerifierMode is how far the draft verifier checks a capture against its
// grounding reading.
type VerifierMode string

const (
	VerifierOff   VerifierMode = "off"
	VerifierProse VerifierMode = "prose"
	VerifierDeep  VerifierMode = "deep"
)

// VerifierSkip is when the deep exchange may be skipped, ordered from the
// loosest to the strictest.
type VerifierSkip string

const (
	VerifierSkipWeighed VerifierSkip = "weighed"
	VerifierSkipFailure VerifierSkip = "failure"
	VerifierSkipNever   VerifierSkip = "never"
)

// AllowedValues lists the skips, loosest first.
func (VerifierSkip) AllowedValues() []string {
	return closedValues(VerifierSkipWeighed, VerifierSkipFailure, VerifierSkipNever)
}

// VerifierRunner is what runs a deep verifier session (V-D16, V-D19): the
// drafting agent's harness as a subagent, or sdd through a headless agent CLI
// or an external driver.
type VerifierRunner string

const (
	VerifierRunnerSubagent VerifierRunner = "subagent"
	VerifierRunnerClaude   VerifierRunner = "claude"
	VerifierRunnerCodex    VerifierRunner = "codex"
	VerifierRunnerDriver   VerifierRunner = "driver"
)

func (VerifierRunner) AllowedValues() []string {
	return closedValues(VerifierRunnerSubagent, VerifierRunnerClaude, VerifierRunnerCodex, VerifierRunnerDriver)
}

// VerifierDriver is the external command a driver runner runs per round.
type VerifierDriver struct {
	Command string   `yaml:"command,omitempty"`
	Args    []string `yaml:"args,omitempty"`
}

// VerifierAgent is the model and effort the deep verifier runs with in one
// agent family, passed to its CLI or subagent tool as written; an empty value
// leaves the agent's own default. A driver's command picks its own.
type VerifierAgent struct {
	Model  string `yaml:"model,omitempty"`
	Effort string `yaml:"effort,omitempty"`
}

// Verifier agent defaults (20261109-222220-d-tac-4ev): the alias sonnet, which
// every Claude harness accepts, and codex's own default model.
var (
	DefaultVerifierClaude = VerifierAgent{Model: "sonnet", Effort: "medium"}
	DefaultVerifierCodex  = VerifierAgent{Effort: "medium"}
)

// ClaudeEffort is a level the claude CLI's --effort takes. Codex accepts
// model-defined levels, so its effort stays a plain string.
type ClaudeEffort string

func (ClaudeEffort) AllowedValues() []string {
	return []string{"low", "medium", "high", "xhigh", "max"}
}

// ClaudeAgent is the verify.claude block: a VerifierAgent whose effort the
// claude CLI must take.
type ClaudeAgent struct {
	Model  string       `yaml:"model,omitempty"`
	Effort ClaudeEffort `yaml:"effort,omitempty"`
}

// VerifierAgent returns the block as the policy carries it.
func (a ClaudeAgent) VerifierAgent() VerifierAgent {
	return VerifierAgent{Model: a.Model, Effort: string(a.Effort)}
}

// VerifyConfig is the `verify` block (20261109-004506-d-cpt-udz,
// 20261109-004729-d-cpt-xs5, 20261109-222220-d-tac-4ev). Mode and skip are
// policy: the user-global and the committed config set them, the committed
// value wins, and the machine-local config may only tighten them. The runner
// and the per-agent model and effort describe the machine, so the
// machine-local value wins (V-D20); the driver runs a command, so only the
// user-global and machine-local layers set it (V-D19). Pointers tell an unset
// grounding key from one a layer sets.
type VerifyConfig struct {
	Mode          VerifyMode           `yaml:"mode,omitempty"`
	Skip          VerifierSkip         `yaml:"skip,omitempty"`
	Runner        VerifierRunner       `yaml:"runner,omitempty"`
	Driver        VerifierDriver       `yaml:"driver,omitempty"`
	Claude        ClaudeAgent          `yaml:"claude,omitempty"`
	Codex         VerifierAgent        `yaml:"codex,omitempty"`
	Depth         GroundingDepthConfig `yaml:"depth,omitempty"`
	BriefAllBelow *int                 `yaml:"brief_all_below,omitempty"`
	PrefilterFrom *int                 `yaml:"prefilter_from,omitempty"`
	// Prefilter switches the prefilter on (D7); on by default.
	Prefilter *bool `yaml:"prefilter,omitempty"`
}

// VerifierPolicy is the resolved verifier configuration of one project.
type VerifierPolicy struct {
	Mode   VerifierMode
	Skip   VerifierSkip
	Runner VerifierRunner
	Driver VerifierDriver
	Claude VerifierAgent
	Codex  VerifierAgent
}

// VerifierPolicy resolves the block's verifier keys against the defaults.
func (c VerifyConfig) VerifierPolicy() VerifierPolicy {
	_, mode := c.ResolvedMode().Settings()
	return VerifierPolicy{
		Mode: mode, Skip: c.Skip, Runner: c.Runner, Driver: c.Driver,
		Claude: c.Claude.VerifierAgent(), Codex: c.Codex,
	}.WithDefaults()
}

// WithDefaults fills the unset values: off, skip on failure, a subagent
// runner, and the default model and effort of each agent family.
func (p VerifierPolicy) WithDefaults() VerifierPolicy {
	if p.Mode == "" {
		p.Mode = VerifierOff
	}
	if p.Skip == "" {
		p.Skip = VerifierSkipFailure
	}
	if p.Runner == "" {
		p.Runner = VerifierRunnerSubagent
	}
	p.Claude = mergeVerifierAgent(DefaultVerifierClaude, p.Claude)
	p.Codex = mergeVerifierAgent(DefaultVerifierCodex, p.Codex)
	return p
}

func mergeVerifyConfig(base, overlay VerifyConfig) VerifyConfig {
	out := base
	if overlay.Mode != "" {
		out.Mode = overlay.Mode
	}
	if overlay.Skip != "" {
		out.Skip = overlay.Skip
	}
	if overlay.Runner != "" {
		out.Runner = overlay.Runner
	}
	if overlay.Driver.Command != "" {
		out.Driver = overlay.Driver
	}
	if overlay.Claude.Model != "" {
		out.Claude.Model = overlay.Claude.Model
	}
	if overlay.Claude.Effort != "" {
		out.Claude.Effort = overlay.Claude.Effort
	}
	out.Codex = mergeVerifierAgent(out.Codex, overlay.Codex)
	if overlay.Depth.Down != nil {
		out.Depth.Down = overlay.Depth.Down
	}
	if overlay.Depth.Up != nil {
		out.Depth.Up = overlay.Depth.Up
	}
	if overlay.BriefAllBelow != nil {
		out.BriefAllBelow = overlay.BriefAllBelow
	}
	if overlay.Prefilter != nil {
		out.Prefilter = overlay.Prefilter
	}
	if overlay.PrefilterFrom != nil {
		out.PrefilterFrom = overlay.PrefilterFrom
	}
	return out
}

func mergeVerifierAgent(base, overlay VerifierAgent) VerifierAgent {
	if overlay.Model != "" {
		base.Model = overlay.Model
	}
	if overlay.Effort != "" {
		base.Effort = overlay.Effort
	}
	return base
}

// CheckVerifyLayers refuses a verify configuration that cannot hold: an
// unknown value, a machine-local value loosening what the user-global and
// committed layers set, a driver in the committed config, or a driver
// runner no layer names a command for (V-D19). files names the file behind
// each layer; the refusal names the key, and the file of every layer it
// judges.
func CheckVerifyLayers(global BaseConfig, committed, local *PerRepoConfig, files map[ConfigValueSource]string) error {
	problems := verifyLayerProblems(global, committed, local, files)
	if len(problems) == 0 {
		return nil
	}
	return problems[0].err
}

// VerifyKeyRefusal is CheckVerifyLayers judging only key: the first refusal
// under key or a block holding it, so a value elsewhere never blocks a write
// to key (20260921-174543-d-cpt-tv9).
func VerifyKeyRefusal(key string, global BaseConfig, committed, local *PerRepoConfig, files map[ConfigValueSource]string) error {
	for _, problem := range verifyLayerProblems(global, committed, local, files) {
		if key == problem.key || strings.HasPrefix(key, problem.key+".") {
			return problem.err
		}
	}
	return nil
}

type verifyProblem struct {
	key string
	err error
}

func verifyLayerProblems(global BaseConfig, committed, local *PerRepoConfig, files map[ConfigValueSource]string) []verifyProblem {
	var problems []verifyProblem
	refuse := func(key string, err error) {
		problems = append(problems, verifyProblem{key: key, err: err})
	}
	layers := []struct {
		source ConfigValueSource
		cfg    BaseConfig
	}{{ConfigSourceGlobal, global}}
	if committed != nil {
		layers = append(layers, struct {
			source ConfigValueSource
			cfg    BaseConfig
		}{ConfigSourceProject, committed.BaseConfig})
	}
	if local != nil {
		layers = append(layers, struct {
			source ConfigValueSource
			cfg    BaseConfig
		}{ConfigSourceLocal, local.BaseConfig})
	}
	for _, layer := range layers {
		verify := layer.cfg.Verify
		for _, setting := range []struct {
			key, value string
			allowed    []string
		}{
			{"verify.mode", string(verify.Mode), verify.Mode.AllowedValues()},
			{"verify.skip", string(verify.Skip), verify.Skip.AllowedValues()},
			{"verify.runner", string(verify.Runner), verify.Runner.AllowedValues()},
			{"verify.claude.effort", string(verify.Claude.Effort), verify.Claude.Effort.AllowedValues()},
		} {
			if setting.value == "" {
				continue
			}
			if err := checkClosedValue(setting.key, setting.value, setting.allowed); err != nil {
				refuse(setting.key, fmt.Errorf("%s: %w", layerFile(layer.source, files), err))
			}
		}
		if layer.source == ConfigSourceProject && verify.Driver.Command != "" {
			refuse("verify.driver", fmt.Errorf("%s: verify.driver is refused in the committed config, since its command would run on every contributor's machine; set it in the user-global or the machine-local config", layerFile(layer.source, files)))
		}
	}
	shared := MergeBaseConfig(global, BaseConfig{})
	if committed != nil {
		shared = MergeBaseConfig(shared, committed.BaseConfig)
	}
	if local != nil {
		lowerMode := shared.Verify.Mode
		if lowerMode == "" {
			lowerMode = DefaultVerifyMode
		}
		lowerSkip := shared.Verify.VerifierPolicy().Skip
		if mode := local.Verify.Mode; mode != "" && strictness(mode) < strictness(lowerMode) {
			refuse("verify.mode", fmt.Errorf("%s: verify.mode %s is refused in the machine-local config, which may only raise the %s the shared layers resolve to; lower it in %s",
				layerFile(ConfigSourceLocal, files), mode, lowerMode, sharedVerifyFile(global, committed, "mode", files)))
		}
		if skip := local.Verify.Skip; skip != "" && strictness(skip) < strictness(lowerSkip) {
			refuse("verify.skip", fmt.Errorf("%s: verify.skip %s is refused in the machine-local config, which may only tighten the %s the shared layers set toward never; loosen it in %s",
				layerFile(ConfigSourceLocal, files), skip, lowerSkip, sharedVerifyFile(global, committed, "skip", files)))
		}
	}
	resolved := MergeBaseConfig(shared, BaseConfig{})
	if local != nil {
		resolved = MergeBaseConfig(resolved, local.BaseConfig)
	}
	if policy := resolved.Verify.VerifierPolicy(); policy.Runner == VerifierRunnerDriver && policy.Driver.Command == "" {
		refuse("verify.runner", fmt.Errorf("verify.runner driver needs verify.driver.command, which neither the user-global nor the machine-local config sets; set it in %s", layerFile(ConfigSourceLocal, files)))
	}
	return problems
}

// strictness is a value's place in its type's AllowedValues, which lists the
// loosest first.
func strictness[S interface {
	~string
	ClosedValues
}](value S) int {
	return slices.Index(value.AllowedValues(), string(value))
}

func layerFile(source ConfigValueSource, files map[ConfigValueSource]string) string {
	if file := files[source]; file != "" {
		return file
	}
	return string(source) + " config"
}

// sharedVerifyFile names the shared layer whose value a local one would
// loosen: the committed config when it sets the key, else the user-global.
func sharedVerifyFile(global BaseConfig, committed *PerRepoConfig, key string, files map[ConfigValueSource]string) string {
	if committed != nil {
		if (key == "mode" && committed.Verify.Mode != "") || (key == "skip" && committed.Verify.Skip != "") {
			return layerFile(ConfigSourceProject, files)
		}
	}
	if (key == "mode" && global.Verify.Mode != "") || (key == "skip" && global.Verify.Skip != "") {
		return layerFile(ConfigSourceGlobal, files)
	}
	return layerFile(ConfigSourceProject, files)
}

// VerifierRunnerChoices are the runners `sdd init` offers for the deep
// verifier (V-D28): subagent first as the default, then claude and codex where
// agents names them, in its order, and driver only where a layer already
// configures a driver command.
func VerifierRunnerChoices(agents []AgentTarget, driver VerifierDriver) []VerifierRunner {
	choices := []VerifierRunner{VerifierRunnerSubagent}
	for _, agent := range agents {
		runner := VerifierRunner(agent)
		if (runner == VerifierRunnerClaude || runner == VerifierRunnerCodex) && !slices.Contains(choices, runner) {
			choices = append(choices, runner)
		}
	}
	if driver.Command != "" {
		choices = append(choices, VerifierRunnerDriver)
	}
	return choices
}

// VerifierRunnerAsked reports whether `sdd init` asks for the runner (V-D28):
// only under mode deep, and only while no layer sets one.
func VerifierRunnerAsked(mode VerifyMode, recorded VerifyConfig) bool {
	return mode == VerifyDeep && recorded.Runner == ""
}

// SubagentAgent is the agent block a subagent runner launches the deep
// verifier with under the MCP client clientName: Claude's where the name holds
// "claude", Codex's where it holds "codex", matched case-insensitively; none
// for any other client, whose subagent tool takes the harness's own.
func (p VerifierPolicy) SubagentAgent(clientName string) VerifierAgent {
	name := strings.ToLower(clientName)
	switch {
	case strings.Contains(name, "claude"):
		return p.Claude
	case strings.Contains(name, "codex"):
		return p.Codex
	}
	return VerifierAgent{}
}

// ResolvedMode is the block's mode, DefaultVerifyMode when unset.
func (c VerifyConfig) ResolvedMode() VerifyMode {
	if c.Mode == "" {
		return DefaultVerifyMode
	}
	return c.Mode
}
