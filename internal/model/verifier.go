package model

import (
	"fmt"
	"slices"
	"strconv"
)

// VerifierMode is how far the draft verifier checks a capture against its
// grounding reading (20261109-004729-d-cpt-xs5). The modes build up, so their
// order is the strictness a machine-local layer may only raise.
type VerifierMode string

const (
	VerifierOff   VerifierMode = "off"
	VerifierLight VerifierMode = "light"
	VerifierDeep  VerifierMode = "deep"
)

var verifierModes = []VerifierMode{VerifierOff, VerifierLight, VerifierDeep}

// VerifierSkip is when the deep exchange may be skipped, ordered from the
// loosest to the strictest.
type VerifierSkip string

const (
	VerifierSkipWeighed VerifierSkip = "weighed"
	VerifierSkipFailure VerifierSkip = "failure"
	VerifierSkipNever   VerifierSkip = "never"
)

var verifierSkips = []VerifierSkip{VerifierSkipWeighed, VerifierSkipFailure, VerifierSkipNever}

// VerifierConfig is the `verifier` block. Mode and skip layer like
// grounding.enforce: the user-global and the committed config set them, the
// committed value wins, and the machine-local config may only tighten them.
type VerifierConfig struct {
	Mode VerifierMode `yaml:"mode,omitempty"`
	Skip VerifierSkip `yaml:"skip,omitempty"`
}

// VerifierPolicy is the resolved verifier configuration of one project.
type VerifierPolicy struct {
	Mode VerifierMode
	Skip VerifierSkip
}

// Policy resolves the block against the defaults: off, and skip on failure.
func (c VerifierConfig) Policy() VerifierPolicy {
	policy := VerifierPolicy(c)
	if policy.Mode == "" {
		policy.Mode = VerifierOff
	}
	if policy.Skip == "" {
		policy.Skip = VerifierSkipFailure
	}
	return policy
}

func mergeVerifierConfig(base, overlay VerifierConfig) VerifierConfig {
	out := base
	if overlay.Mode != "" {
		out.Mode = overlay.Mode
	}
	if overlay.Skip != "" {
		out.Skip = overlay.Skip
	}
	return out
}

// CheckVerifierLayers refuses a verifier configuration that cannot hold:
// an unknown value, a machine-local value loosening what the user-global and
// committed layers set, or a verifier mode other than off while grounding
// enforcement is off (V-D7). files names the file behind each layer; the
// refusal names the key, and the file of every layer it judges.
func CheckVerifierLayers(global BaseConfig, committed, local *PerRepoConfig, files map[ConfigValueSource]string) error {
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
		if mode := layer.cfg.Verifier.Mode; mode != "" && !slices.Contains(verifierModes, mode) {
			return fmt.Errorf("%s: verifier.mode %q is not off, light or deep", layerFile(layer.source, files), mode)
		}
		if skip := layer.cfg.Verifier.Skip; skip != "" && !slices.Contains(verifierSkips, skip) {
			return fmt.Errorf("%s: verifier.skip %q is not never, failure or weighed", layerFile(layer.source, files), skip)
		}
	}
	shared := MergeBaseConfig(global, BaseConfig{})
	if committed != nil {
		shared = MergeBaseConfig(shared, committed.BaseConfig)
	}
	if local != nil {
		lower := shared.Verifier.Policy()
		if mode := local.Verifier.Mode; mode != "" && slices.Index(verifierModes, mode) < slices.Index(verifierModes, lower.Mode) {
			return fmt.Errorf("%s: verifier.mode %s is refused in the machine-local config, which may only raise the %s the shared layers set; lower it in %s",
				layerFile(ConfigSourceLocal, files), mode, lower.Mode, sharedVerifierFile(global, committed, "mode", files))
		}
		if skip := local.Verifier.Skip; skip != "" && slices.Index(verifierSkips, skip) < slices.Index(verifierSkips, lower.Skip) {
			return fmt.Errorf("%s: verifier.skip %s is refused in the machine-local config, which may only tighten the %s the shared layers set toward never; loosen it in %s",
				layerFile(ConfigSourceLocal, files), skip, lower.Skip, sharedVerifierFile(global, committed, "skip", files))
		}
	}
	values := EffectiveConfigValues(global, committed, local)
	mode, enforce := configValue(values, "verifier.mode"), configValue(values, "grounding.enforce")
	if mode.Value == "" || VerifierMode(mode.Value) == VerifierOff {
		return nil
	}
	if enforced, _ := strconv.ParseBool(enforce.Value); enforced {
		return nil
	}
	enforceFrom := "unset, off by default"
	if enforce.Source != "" && enforce.Source != ConfigSourceDefault {
		enforceFrom = "false in " + layerFile(enforce.Source, files)
	}
	return fmt.Errorf("verifier.mode %s (set in %s) requires grounding.enforce: true, which is %s; set grounding.enforce: true or verifier.mode: off",
		mode.Value, layerFile(mode.Source, files), enforceFrom)
}

func configValue(values []ConfigValue, key string) ConfigValue {
	for _, value := range values {
		if value.Key == key {
			return value
		}
	}
	return ConfigValue{Key: key}
}

func layerFile(source ConfigValueSource, files map[ConfigValueSource]string) string {
	if file := files[source]; file != "" {
		return file
	}
	return string(source) + " config"
}

// sharedVerifierFile names the shared layer whose value a local one would
// loosen: the committed config when it sets the key, else the user-global.
func sharedVerifierFile(global BaseConfig, committed *PerRepoConfig, key string, files map[ConfigValueSource]string) string {
	if committed != nil {
		if (key == "mode" && committed.Verifier.Mode != "") || (key == "skip" && committed.Verifier.Skip != "") {
			return layerFile(ConfigSourceProject, files)
		}
	}
	if (key == "mode" && global.Verifier.Mode != "") || (key == "skip" && global.Verifier.Skip != "") {
		return layerFile(ConfigSourceGlobal, files)
	}
	return layerFile(ConfigSourceProject, files)
}

// Strictness is the one answer to how strictly drafts are checked against
// the graph, which sets grounding.enforce and verifier.mode together so no
// invalid pair can be chosen (V-D28).
type Strictness string

const (
	StrictnessOff       Strictness = "off"
	StrictnessGrounding Strictness = "grounding"
	StrictnessLight     Strictness = "light"
	StrictnessDeep      Strictness = "deep"
)

// Strictnesses lists the answers, loosest first.
func Strictnesses() []Strictness {
	return []Strictness{StrictnessOff, StrictnessGrounding, StrictnessLight, StrictnessDeep}
}

// ParseStrictness reads an answer, refusing one outside the set.
func ParseStrictness(value string) (Strictness, error) {
	s := Strictness(value)
	if !slices.Contains(Strictnesses(), s) {
		return "", fmt.Errorf("strictness %q is not off, grounding, light or deep", value)
	}
	return s, nil
}

// Settings returns the grounding.enforce and verifier.mode the answer sets.
func (s Strictness) Settings() (enforce bool, mode VerifierMode) {
	switch s {
	case StrictnessGrounding:
		return true, VerifierOff
	case StrictnessLight:
		return true, VerifierLight
	case StrictnessDeep:
		return true, VerifierDeep
	}
	return false, VerifierOff
}
