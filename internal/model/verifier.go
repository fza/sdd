package model

import (
	"fmt"
	"slices"
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

// VerifyModes lists the modes, loosest first.
func VerifyModes() []VerifyMode {
	return []VerifyMode{VerifyOff, VerifyGrounding, VerifyProse, VerifyDeep}
}

// ParseVerifyMode reads a mode, refusing one outside the set.
func ParseVerifyMode(value string) (VerifyMode, error) {
	mode := VerifyMode(value)
	if !slices.Contains(VerifyModes(), mode) {
		return "", fmt.Errorf("verify.mode %q is not off, grounding, prose or deep", value)
	}
	return mode, nil
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

var verifierSkips = []VerifierSkip{VerifierSkipWeighed, VerifierSkipFailure, VerifierSkipNever}

// VerifierLaunch is how a deep verifier session is launched (V-D16, V-D19):
// by the drafting agent's harness as a subagent, or by sdd through a headless
// agent CLI or an external driver.
type VerifierLaunch string

const (
	VerifierLaunchSubagent     VerifierLaunch = "subagent"
	VerifierLaunchClaude       VerifierLaunch = "claude"
	VerifierLaunchCodex        VerifierLaunch = "codex"
	VerifierLaunchCustomDriver VerifierLaunch = "custom_driver"
)

var verifierLaunches = []VerifierLaunch{VerifierLaunchSubagent, VerifierLaunchClaude, VerifierLaunchCodex, VerifierLaunchCustomDriver}

// VerifierDriver is the external command a custom_driver launch runs per
// round.
type VerifierDriver struct {
	Command string   `yaml:"command,omitempty"`
	Args    []string `yaml:"args,omitempty"`
}

// VerifierAgent is the model and effort one launch runs the deep verifier
// with; an empty value leaves the launch's own default. Each launch names
// models and efforts its own way, so every launch has its own pair, and a
// custom_driver's command picks its own.
type VerifierAgent struct {
	Model  string `yaml:"model,omitempty"`
	Effort string `yaml:"effort,omitempty"`
}

// claudeEfforts are the levels the claude CLI's --effort takes. Codex and a
// harness's subagent tool accept model-defined levels, so theirs pass as set.
var claudeEfforts = []string{"low", "medium", "high", "xhigh", "max"}

// VerifyConfig is the `verify` block (20261109-004506-d-cpt-udz,
// 20261109-004729-d-cpt-xs5). Mode and skip are policy: the user-global and
// the committed config set them, the committed value wins, and the
// machine-local config may only tighten them. Launch and the per-launch
// model and effort describe the machine, so the machine-local value wins
// (V-D20); the driver runs a command, so only the user-global and
// machine-local layers set it (V-D19). Pointers tell an unset grounding key
// from one a layer sets.
type VerifyConfig struct {
	Mode          VerifyMode           `yaml:"mode,omitempty"`
	Skip          VerifierSkip         `yaml:"skip,omitempty"`
	Launch        VerifierLaunch       `yaml:"launch,omitempty"`
	Driver        VerifierDriver       `yaml:"driver,omitempty"`
	Subagent      VerifierAgent        `yaml:"subagent,omitempty"`
	Claude        VerifierAgent        `yaml:"claude,omitempty"`
	Codex         VerifierAgent        `yaml:"codex,omitempty"`
	Depth         GroundingDepthConfig `yaml:"depth,omitempty"`
	BriefAllBelow *int                 `yaml:"brief_all_below,omitempty"`
	PrefilterFrom *int                 `yaml:"prefilter_from,omitempty"`
	// Prefilter switches the prefilter on where llm_prefilter configures it
	// (D7); off by default.
	Prefilter *bool `yaml:"prefilter,omitempty"`
}

// VerifierPolicy is the resolved verifier configuration of one project.
type VerifierPolicy struct {
	Mode     VerifierMode
	Skip     VerifierSkip
	Launch   VerifierLaunch
	Driver   VerifierDriver
	Subagent VerifierAgent
	Claude   VerifierAgent
	Codex    VerifierAgent
}

// VerifierPolicy resolves the block's verifier keys against the defaults.
func (c VerifyConfig) VerifierPolicy() VerifierPolicy {
	_, mode := c.Mode.Settings()
	return VerifierPolicy{
		Mode: mode, Skip: c.Skip, Launch: c.Launch, Driver: c.Driver,
		Subagent: c.Subagent, Claude: c.Claude, Codex: c.Codex,
	}.WithDefaults()
}

// WithDefaults fills the unset values: off, skip on failure, and a subagent
// launch.
func (p VerifierPolicy) WithDefaults() VerifierPolicy {
	if p.Mode == "" {
		p.Mode = VerifierOff
	}
	if p.Skip == "" {
		p.Skip = VerifierSkipFailure
	}
	if p.Launch == "" {
		p.Launch = VerifierLaunchSubagent
	}
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
	if overlay.Launch != "" {
		out.Launch = overlay.Launch
	}
	if overlay.Driver.Command != "" {
		out.Driver = overlay.Driver
	}
	out.Subagent = mergeVerifierAgent(out.Subagent, overlay.Subagent)
	out.Claude = mergeVerifierAgent(out.Claude, overlay.Claude)
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
// committed layers set, a driver in the committed config, or a custom_driver
// launch no layer names a driver for (V-D19). files names the file behind
// each layer; the refusal names the key, and the file of every layer it
// judges.
func CheckVerifyLayers(global BaseConfig, committed, local *PerRepoConfig, files map[ConfigValueSource]string) error {
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
		if mode := layer.cfg.Verify.Mode; mode != "" {
			_, err := ParseVerifyMode(string(mode))
			if err != nil {
				return fmt.Errorf("%s: %w", layerFile(layer.source, files), err)
			}
		}
		if skip := layer.cfg.Verify.Skip; skip != "" && !slices.Contains(verifierSkips, skip) {
			return fmt.Errorf("%s: verify.skip %q is not never, failure or weighed", layerFile(layer.source, files), skip)
		}
		if launch := layer.cfg.Verify.Launch; launch != "" && !slices.Contains(verifierLaunches, launch) {
			return fmt.Errorf("%s: verify.launch %q is not subagent, claude, codex or custom_driver", layerFile(layer.source, files), launch)
		}
		if effort := layer.cfg.Verify.Claude.Effort; effort != "" && !slices.Contains(claudeEfforts, effort) {
			return fmt.Errorf("%s: verify.claude.effort %q is not low, medium, high, xhigh or max", layerFile(layer.source, files), effort)
		}
		if layer.source == ConfigSourceProject && layer.cfg.Verify.Driver.Command != "" {
			return fmt.Errorf("%s: verify.driver is refused in the committed config, since its command would run on every contributor's machine; set it in the user-global or the machine-local config", layerFile(layer.source, files))
		}
	}
	shared := MergeBaseConfig(global, BaseConfig{})
	if committed != nil {
		shared = MergeBaseConfig(shared, committed.BaseConfig)
	}
	if local != nil {
		lowerMode := shared.Verify.Mode
		if lowerMode == "" {
			lowerMode = VerifyOff
		}
		lowerSkip := shared.Verify.VerifierPolicy().Skip
		if mode := local.Verify.Mode; mode != "" && slices.Index(VerifyModes(), mode) < slices.Index(VerifyModes(), lowerMode) {
			return fmt.Errorf("%s: verify.mode %s is refused in the machine-local config, which may only raise the %s the shared layers set; lower it in %s",
				layerFile(ConfigSourceLocal, files), mode, lowerMode, sharedVerifyFile(global, committed, "mode", files))
		}
		if skip := local.Verify.Skip; skip != "" && slices.Index(verifierSkips, skip) < slices.Index(verifierSkips, lowerSkip) {
			return fmt.Errorf("%s: verify.skip %s is refused in the machine-local config, which may only tighten the %s the shared layers set toward never; loosen it in %s",
				layerFile(ConfigSourceLocal, files), skip, lowerSkip, sharedVerifyFile(global, committed, "skip", files))
		}
	}
	resolved := MergeBaseConfig(shared, BaseConfig{})
	if local != nil {
		resolved = MergeBaseConfig(resolved, local.BaseConfig)
	}
	if policy := resolved.Verify.VerifierPolicy(); policy.Launch == VerifierLaunchCustomDriver && policy.Driver.Command == "" {
		return fmt.Errorf("verify.launch custom_driver needs verify.driver.command, which neither the user-global nor the machine-local config sets; set it in %s", layerFile(ConfigSourceLocal, files))
	}
	return nil
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

// VerifierLaunchChoices are the launches `sdd init` offers for the deep
// verifier (V-D28): subagent first as the default, then claude and codex where
// agents names them, in its order, and custom_driver only where a layer
// already configures a driver.
func VerifierLaunchChoices(agents []AgentTarget, driver VerifierDriver) []VerifierLaunch {
	choices := []VerifierLaunch{VerifierLaunchSubagent}
	for _, agent := range agents {
		launch := VerifierLaunch(agent)
		if (launch == VerifierLaunchClaude || launch == VerifierLaunchCodex) && !slices.Contains(choices, launch) {
			choices = append(choices, launch)
		}
	}
	if driver.Command != "" {
		choices = append(choices, VerifierLaunchCustomDriver)
	}
	return choices
}

// VerifierLaunchAsked reports whether `sdd init` asks for the launch (V-D28):
// only under mode deep, and only while no layer sets one.
func VerifierLaunchAsked(mode VerifyMode, recorded VerifyConfig) bool {
	return mode == VerifyDeep && recorded.Launch == ""
}
