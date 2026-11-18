package model

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// VerifyMode is how carefully a capture is checked against the graph before
// it is written: it sets grounding enforcement and the draft verifier
// together, so no invalid pair can be chosen. The modes build up, so their
// order is the strictness a machine-local layer may only raise.
type VerifyMode string

const (
	VerifyOff       VerifyMode = "off"
	VerifyGrounding VerifyMode = "grounding"
	VerifyProse     VerifyMode = "prose"
	VerifyDeep      VerifyMode = "deep"
)

// DefaultVerifyMode is the mode an unset verify.mode resolves to.
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

// VerifierSkip is when the deep exchange, or a pre-flight answer cut at the
// token cap, may be skipped, ordered from the loosest to the strictest.
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

// VerifierRunner is what runs a deep verifier session: the drafting agent's
// harness as a subagent, or sdd through a headless claude or an external
// driver.
type VerifierRunner string

const (
	VerifierRunnerSubagent VerifierRunner = "subagent"
	VerifierRunnerClaude   VerifierRunner = "claude"
	VerifierRunnerDriver   VerifierRunner = "driver"
)

func (VerifierRunner) AllowedValues() []string {
	return closedValues(VerifierRunnerSubagent, VerifierRunnerClaude, VerifierRunnerDriver)
}

// VerifierDriver is the external command a driver runner runs per round.
type VerifierDriver struct {
	Command string   `yaml:"command,omitempty"`
	Args    []string `yaml:"args,omitempty"`
}

// VerifierAgent is the model and effort the deep verifier runs with, passed
// to the claude CLI or subagent tool as written; an empty value leaves the
// agent's own default. A driver's command picks its own.
type VerifierAgent struct {
	Model  string `yaml:"model,omitempty"`
	Effort string `yaml:"effort,omitempty"`
}

// DefaultVerifierClaude is the verifier agent's default: the alias sonnet,
// which every Claude harness accepts.
var DefaultVerifierClaude = VerifierAgent{Model: "sonnet", Effort: "medium"}

// DefaultVerifierRoundTimeout bounds one round of a delegate sdd launches
// while verify.round_timeout is unset: long enough for a verifier reading
// several pages, short enough that a stalled one surfaces while the user
// still waits.
const DefaultVerifierRoundTimeout = 10 * time.Minute

// DefaultVerifierIdleTimeout is how long a capture waits on a verify
// delegate that makes no call while verify.idle_timeout is unset.
const DefaultVerifierIdleTimeout = 10 * time.Minute

// DefaultRankingTimeout bounds the deep reading's ranking pass while
// verify.ranking_timeout is unset: a chat judge's slowest measured call
// and one retry of it fit.
const DefaultRankingTimeout = 45 * time.Second

// ResolvedRankingTimeout is how long the deep reading waits for its ranking
// judge, from the pass's start: verify.ranking_timeout, else
// DefaultRankingTimeout.
func (c VerifyConfig) ResolvedRankingTimeout() time.Duration {
	if d, ok := c.RankingTimeout.Duration(); ok {
		return d
	}
	return DefaultRankingTimeout
}

// ClaudeEffort is a level the claude CLI's --effort takes.
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

// VerifyConfig is the `verify` block. Mode and skip are policy: the
// user-global and the committed config set them, the committed value wins,
// and the machine-local config may only tighten them. The runner, the
// per-agent model and effort and the round and idle timeouts, Go durations,
// describe the machine, so the machine-local value wins; the driver runs a command, so
// only the user-global and machine-local layers set it. Pointers tell an
// unset grounding key from one a layer sets.
type VerifyConfig struct {
	Mode         VerifyMode     `yaml:"mode,omitempty"`
	Skip         VerifierSkip   `yaml:"skip,omitempty"`
	Runner       VerifierRunner `yaml:"runner,omitempty"`
	Driver       VerifierDriver `yaml:"driver,omitempty"`
	Claude       ClaudeAgent    `yaml:"claude,omitempty"`
	RoundTimeout VerifyDuration `yaml:"round_timeout,omitempty"`
	IdleTimeout  VerifyDuration `yaml:"idle_timeout,omitempty"`
	// RankingTimeout bounds the deep reading's ranking pass
	// (ResolvedRankingTimeout).
	RankingTimeout VerifyDuration       `yaml:"ranking_timeout,omitempty"`
	Depth          GroundingDepthConfig `yaml:"depth,omitempty"`
	BriefAllBelow  *int                 `yaml:"brief_all_below,omitempty"`
	PrefilterFrom  *int                 `yaml:"prefilter_from,omitempty"`
	// Prefilter switches the prefilter on; on by default.
	Prefilter *bool `yaml:"prefilter,omitempty"`
	// Kinds sets the mode per entry kind; see ModeFor.
	Kinds map[Kind]VerifyMode `yaml:"kinds,omitempty"`
}

// UnmarshalYAML refuses the verify.codex block a former sdd read: unlike a
// key a newer sdd may write, it names a verifier sdd no longer runs.
func (c *VerifyConfig) UnmarshalYAML(node *yaml.Node) error {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "codex" {
			return errors.New("verify.codex no longer exists, since deep verification needs Claude Code; remove the block")
		}
	}
	type plain VerifyConfig
	return node.Decode((*plain)(c))
}

// cappedKinds are checked at most at prose while Kinds names no kind: they
// make no claim a deep check can test, or a deep run costs more than it finds.
var cappedKinds = []Kind{KindDone, KindActor, KindRole, KindFocus, KindAnnotation, KindInsight, KindQuestion, KindActivity, KindProcedure}

// ModeFor is the mode a draft of kind is checked at: the mode Kinds names
// for it, else Mode. While Kinds names no kind, cappedKinds take at most
// prose.
func (c VerifyConfig) ModeFor(kind Kind) VerifyMode {
	c = c.KnownValues()
	mode := c.ResolvedMode()
	if len(c.Kinds) > 0 {
		if named, ok := c.Kinds[kind]; ok {
			return named
		}
		return mode
	}
	if slices.Contains(cappedKinds, kind) && strictness(mode) > strictness(VerifyProse) {
		return VerifyProse
	}
	return mode
}

// StricterKinds lists the kinds checked more strictly than kind: those an
// unnamed kind's Mode or a named kind's own mode puts above it; nil when
// none is. The drafter picks the kind, so pre-flight tests that such a draft
// fits it (KindFit).
func (c VerifyConfig) StricterKinds(kind Kind) []Kind {
	own := c.ModeFor(kind)
	var stricter []Kind
	for _, other := range append(SignalKindValues(), DecisionKindValues()...) {
		if strictness(c.ModeFor(other)) > strictness(own) {
			stricter = append(stricter, other)
		}
	}
	return stricter
}

// KindFit asks pre-flight whether a draft fits its kind, for a kind the
// project checks less strictly than another: the drafter picks the kind,
// so a misfit would let a draft dodge the stricter check.
type KindFit struct {
	Kind     Kind
	Question string
	// Stricter are the kinds checked more strictly, each with the question
	// it answers.
	Stricter []KindTest
}

// KindTest is an entry kind and the one question it answers.
type KindTest struct {
	Kind     Kind
	Question string
}

// AllowedValues lists the entry kinds, signals first.
func (Kind) AllowedValues() []string {
	return closedValues(append(SignalKindValues(), DecisionKindValues()...)...)
}

// VerifierPolicy is the resolved verifier configuration of one project.
type VerifierPolicy struct {
	Mode         VerifierMode
	Skip         VerifierSkip
	Runner       VerifierRunner
	Driver       VerifierDriver
	Claude       VerifierAgent
	RoundTimeout time.Duration
	IdleTimeout  time.Duration
}

// VerifierPolicy resolves the block's verifier keys against the defaults.
func (c VerifyConfig) VerifierPolicy() VerifierPolicy {
	c = c.KnownValues()
	_, mode := c.ResolvedMode().Settings()
	timeout, _ := c.RoundTimeout.Duration()
	idle, _ := c.IdleTimeout.Duration()
	return VerifierPolicy{
		Mode: mode, Skip: c.Skip, Runner: c.Runner, Driver: c.Driver,
		Claude: c.Claude.VerifierAgent(), RoundTimeout: timeout, IdleTimeout: idle,
	}.WithDefaults()
}

// WithDefaults fills the unset values: off, skip on failure, a subagent
// runner, the default verifier model and effort, and the default round and
// idle timeouts.
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
	if p.RoundTimeout <= 0 {
		p.RoundTimeout = DefaultVerifierRoundTimeout
	}
	if p.IdleTimeout <= 0 {
		p.IdleTimeout = DefaultVerifierIdleTimeout
	}
	return p
}

// KnownValues clears every closed value outside its set, so a value a newer
// sdd wrote or a typo leaves its key to the layer below
// (20260921-174543-d-cpt-tv9); IgnoredSettings reports it.
func (c VerifyConfig) KnownValues() VerifyConfig {
	if !isClosedValue(c.Mode) {
		c.Mode = ""
	}
	if !isClosedValue(c.Skip) {
		c.Skip = ""
	}
	if !isClosedValue(c.Runner) {
		c.Runner = ""
	}
	if !isClosedValue(c.Claude.Effort) {
		c.Claude.Effort = ""
	}
	if !c.RoundTimeout.Known() {
		c.RoundTimeout = ""
	}
	if !c.IdleTimeout.Known() {
		c.IdleTimeout = ""
	}
	if !c.RankingTimeout.Known() {
		c.RankingTimeout = ""
	}
	if len(c.Kinds) > 0 {
		known := make(map[Kind]VerifyMode, len(c.Kinds))
		for kind, mode := range c.Kinds {
			if isClosedValue(kind) && isClosedValue(mode) {
				known[kind] = mode
			}
		}
		c.Kinds = known
	}
	return c
}

func isClosedValue[S interface {
	~string
	ClosedValues
}](value S) bool {
	return slices.Contains(value.AllowedValues(), string(value))
}

// VerifyDuration is a verify.round_timeout, idle_timeout or ranking_timeout
// value: a duration such as 10m.
type VerifyDuration string

func (VerifyDuration) FreeText() {}

// Known reports whether the value is unset or a positive duration.
func (r VerifyDuration) Known() bool {
	_, ok := r.Duration()
	return r == "" || ok
}

// Duration is the value as a positive duration; false when it is none.
func (r VerifyDuration) Duration() (time.Duration, bool) {
	d, err := time.ParseDuration(string(r))
	return d, err == nil && d > 0
}

func ignoredVerifyValues(c VerifyConfig) []IgnoredSetting {
	var ignored []IgnoredSetting
	for _, setting := range []struct {
		key, value string
		allowed    []string
	}{
		{"verify.mode", string(c.Mode), c.Mode.AllowedValues()},
		{"verify.skip", string(c.Skip), c.Skip.AllowedValues()},
		{"verify.runner", string(c.Runner), c.Runner.AllowedValues()},
		{"verify.claude.effort", string(c.Claude.Effort), c.Claude.Effort.AllowedValues()},
	} {
		if setting.value != "" && !slices.Contains(setting.allowed, setting.value) {
			ignored = append(ignored, IgnoredSetting{Key: setting.key, Reason: fmt.Sprintf("%q is not %s", setting.value, oneOf(setting.allowed))})
		}
	}
	if !c.RoundTimeout.Known() {
		ignored = append(ignored, IgnoredSetting{Key: "verify.round_timeout", Reason: fmt.Sprintf("%q is no positive duration such as 10m", c.RoundTimeout)})
	}
	if !c.IdleTimeout.Known() {
		ignored = append(ignored, IgnoredSetting{Key: "verify.idle_timeout", Reason: fmt.Sprintf("%q is no positive duration such as 10m", c.IdleTimeout)})
	}
	if !c.RankingTimeout.Known() {
		ignored = append(ignored, IgnoredSetting{Key: "verify.ranking_timeout", Reason: fmt.Sprintf("%q is no positive duration such as 45s", c.RankingTimeout)})
	}
	for _, kind := range slices.Sorted(maps.Keys(c.Kinds)) {
		mode, key := c.Kinds[kind], "verify.kinds."+string(kind)
		switch {
		case !isClosedValue(kind):
			ignored = append(ignored, IgnoredSetting{Key: key, Reason: fmt.Sprintf("%q is not an entry kind", kind)})
		case !isClosedValue(mode):
			ignored = append(ignored, IgnoredSetting{Key: key, Reason: fmt.Sprintf("%q is not %s", mode, oneOf(mode.AllowedValues()))})
		}
	}
	return ignored
}

func mergeVerifyConfig(base, overlay VerifyConfig) VerifyConfig {
	out := base.KnownValues()
	overlay = overlay.KnownValues()
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
	if overlay.RoundTimeout != "" {
		out.RoundTimeout = overlay.RoundTimeout
	}
	if overlay.IdleTimeout != "" {
		out.IdleTimeout = overlay.IdleTimeout
	}
	if overlay.RankingTimeout != "" {
		out.RankingTimeout = overlay.RankingTimeout
	}
	if len(overlay.Kinds) > 0 {
		// KnownValues gave out a map of its own, so base stays unchanged.
		if out.Kinds == nil {
			out.Kinds = make(map[Kind]VerifyMode, len(overlay.Kinds))
		}
		maps.Copy(out.Kinds, overlay.Kinds)
	}
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

func knownVerifyLayer(layer *PerRepoConfig) *PerRepoConfig {
	if layer == nil {
		return nil
	}
	known := *layer
	known.Verify = layer.Verify.KnownValues()
	return &known
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

// LayerSetting is an ignored setting with the layer it stands in.
type LayerSetting struct {
	Source ConfigValueSource
	IgnoredSetting
}

// HonoredVerifyLayers drops the known verify values a layer may not set where
// it stands: a machine-local value loosening what the user-global and
// committed layers set, a driver in the committed config, a driver runner no
// layer names a command for (V-D19). A read resolves the rest and warns of
// each one it dropped, so one misplaced value never stops sdd for everyone
// reading the layer (20260921-174543-d-cpt-tv9); a write refuses it
// (VerifyKeyRefusal). files names the file behind each layer; each reason
// names the key and the files it judges.
func HonoredVerifyLayers(global BaseConfig, committed, local *PerRepoConfig, files map[ConfigValueSource]string) (BaseConfig, *PerRepoConfig, *PerRepoConfig, []LayerSetting) {
	global.Verify = global.Verify.KnownValues()
	committed, local = knownVerifyLayer(committed), knownVerifyLayer(local)
	var dropped []LayerSetting
	for {
		problems := verifyLayerProblems(global, committed, local, files)
		if len(problems) == 0 {
			return global, committed, local, dropped
		}
		problem := problems[0]
		source, ok := dropVerifyValue(problem.key, &global, committed, local)
		if !ok {
			return global, committed, local, dropped
		}
		dropped = append(dropped, LayerSetting{Source: source, IgnoredSetting: IgnoredSetting{Key: problem.key, Reason: problem.err.Error()}})
	}
}

// dropVerifyValue clears the value a verify layer problem under key stands
// on, in the layer holding it; false when no layer holds one to clear.
func dropVerifyValue(key string, global *BaseConfig, committed, local *PerRepoConfig) (ConfigValueSource, bool) {
	switch {
	case key == "verify.driver" && committed != nil:
		committed.Verify.Driver = VerifierDriver{}
		return ConfigSourceProject, true
	case key == "verify.mode" && local != nil:
		local.Verify.Mode = ""
		return ConfigSourceLocal, true
	case key == "verify.skip" && local != nil:
		local.Verify.Skip = ""
		return ConfigSourceLocal, true
	case strings.HasPrefix(key, "verify.kinds.") && local != nil:
		kind := Kind(strings.TrimPrefix(key, "verify.kinds."))
		if _, ok := local.Verify.Kinds[kind]; !ok {
			return "", false
		}
		local.Verify.Kinds = maps.Clone(local.Verify.Kinds)
		delete(local.Verify.Kinds, kind)
		return ConfigSourceLocal, true
	case key == "verify.runner":
		switch {
		case local != nil && local.Verify.Runner != "":
			local.Verify.Runner = ""
			return ConfigSourceLocal, true
		case committed != nil && committed.Verify.Runner != "":
			committed.Verify.Runner = ""
			return ConfigSourceProject, true
		case global.Verify.Runner != "":
			global.Verify.Runner = ""
			return ConfigSourceGlobal, true
		}
	}
	return "", false
}

// VerifyKeyRefusal is the write side of HonoredVerifyLayers, unknown values
// included, judging only key: the first refusal under key or a block holding
// it, so a value elsewhere never blocks a write to key
// (20260921-174543-d-cpt-tv9).
func VerifyKeyRefusal(key string, global BaseConfig, committed, local *PerRepoConfig, files map[ConfigValueSource]string) error {
	for _, problem := range verifyLayerProblems(global, committed, local, files) {
		if key == problem.key || strings.HasPrefix(key, problem.key+".") {
			return problem.err
		}
	}
	return nil
}

// removedCodexRunner is a verify.runner value sdd no longer runs, since deep
// verification needs Claude Code. Unlike an unknown value a newer sdd may
// have written, it is refused on read.
const removedCodexRunner VerifierRunner = "codex"

// RefuseRemovedRunner refuses a layer naming the removed codex runner, on
// read as on write; files names the file behind each layer.
func RefuseRemovedRunner(global BaseConfig, committed, local *PerRepoConfig, files map[ConfigValueSource]string) error {
	runners := map[ConfigValueSource]VerifierRunner{ConfigSourceGlobal: global.Verify.Runner}
	if committed != nil {
		runners[ConfigSourceProject] = committed.Verify.Runner
	}
	if local != nil {
		runners[ConfigSourceLocal] = local.Verify.Runner
	}
	for _, source := range []ConfigValueSource{ConfigSourceGlobal, ConfigSourceProject, ConfigSourceLocal} {
		if runners[source] == removedCodexRunner {
			return fmt.Errorf("%s: verify.runner codex no longer exists, since deep verification needs Claude Code; set verify.runner to %s, or remove it",
				layerFile(source, files), oneOf(VerifierRunner("").AllowedValues()))
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
		for _, unknown := range ignoredVerifyValues(verify) {
			refuse(unknown.Key, fmt.Errorf("%s: %s %s", layerFile(layer.source, files), unknown.Key, unknown.Reason))
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
		for _, kind := range append(SignalKindValues(), DecisionKindValues()...) {
			lower, raised := shared.Verify.ModeFor(kind), resolved.Verify.ModeFor(kind)
			if strictness(raised) < strictness(lower) {
				refuse("verify.kinds."+string(kind), fmt.Errorf("%s: verify.kinds.%s %s is refused in the machine-local config, which may only raise the %s the shared layers resolve %s to",
					layerFile(ConfigSourceLocal, files), kind, raised, lower, kind))
			}
		}
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
// verifier: subagent first as the default, then claude where agents names
// it, and driver only where a layer already configures a driver command.
func VerifierRunnerChoices(agents []AgentTarget, driver VerifierDriver) []VerifierRunner {
	choices := []VerifierRunner{VerifierRunnerSubagent}
	for _, agent := range agents {
		runner := VerifierRunner(agent)
		if runner == VerifierRunnerClaude && !slices.Contains(choices, runner) {
			choices = append(choices, runner)
		}
	}
	if driver.Command != "" {
		choices = append(choices, VerifierRunnerDriver)
	}
	return choices
}

// VerifierRunnerAsked reports whether `sdd init` asks for the runner:
// only under mode deep, and only while no layer sets one.
func VerifierRunnerAsked(mode VerifyMode, recorded VerifyConfig) bool {
	return mode == VerifyDeep && recorded.Runner == ""
}

// SubagentAgent is the agent block a subagent runner launches the deep
// verifier with under the MCP client clientName: Claude's for a ClaudeClient,
// none for any other client, whose subagent tool takes the harness's own.
func (p VerifierPolicy) SubagentAgent(clientName string) VerifierAgent {
	if ClaudeClient(clientName) {
		return p.Claude
	}
	return VerifierAgent{}
}

// ResolvedMode is the block's mode, DefaultVerifyMode when unset or unknown.
func (c VerifyConfig) ResolvedMode() VerifyMode {
	if !isClosedValue(c.Mode) {
		return DefaultVerifyMode
	}
	return c.Mode
}
