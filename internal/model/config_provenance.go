package model

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// ConfigValueSource names the overlay layer that supplied an effective value.
type ConfigValueSource string

const (
	ConfigSourceDefault ConfigValueSource = "default"
	ConfigSourceGlobal  ConfigValueSource = "global"
	ConfigSourceProject ConfigValueSource = "project"
	ConfigSourceLocal   ConfigValueSource = "local"
	// ConfigSourceWork and ConfigSourceWorkLocal are the work repository's
	// layers in a project_dir layout, which contribute only hooks.
	ConfigSourceWork      ConfigValueSource = "work"
	ConfigSourceWorkLocal ConfigValueSource = "work local"
)

// ConfigValue is one effective config entry: a dotted key, its rendered
// value, and the layer it came from. Secret marks values that must be masked
// on every output surface (api_keys).
type ConfigValue struct {
	Key    string
	Value  string
	Source ConfigValueSource
	Secret bool
}

// configDefaults are the baked defaults the effective view surfaces when no
// layer sets a key — the values a run actually uses, so `sdd config` answers
// "what will happen" rather than "what is written down".
var configDefaults = map[string]string{
	"graph_dir":            DefaultGraphDir,
	"llm.provider":         DefaultLLMProvider,
	"llm.model":            DefaultLLMModel,
	"llm.effort":           DefaultLLMEffort,
	"llm.max_tokens":       strconv.Itoa(DefaultLLMMaxTokens),
	"llm.concurrency":      strconv.Itoa(DefaultLLMConcurrency),
	"llm.extract_timeout":  DefaultLLMExtractTimeout.String(),
	"llm.debug":            "false",
	"sync.cooldown":        DefaultSyncCooldown,
	"sync.mode":            string(SyncModeManual),
	"dialogue.mode":        string(DialogueGuided),
	"verify.mode":          string(DefaultVerifyMode),
	"verify.skip":          string(VerifierSkipFailure),
	"verify.runner":        string(VerifierRunnerSubagent),
	"verify.prefilter":     strconv.FormatBool(DefaultGroundingPrefilter),
	"verify.claude.model":  DefaultVerifierClaude.Model,
	"verify.claude.effort": DefaultVerifierClaude.Effort,
	"verify.codex.effort":  DefaultVerifierCodex.Effort,

	"sync.default.fetch_cooldown":                 DefaultGitFetchCooldown.String(),
	"sync.default.push_cooldown":                  DefaultGitPushCooldown.String(),
	"sync.default.retries":                        strconv.Itoa(DefaultGitRetries),
	"sync.default.concurrency":                    strconv.Itoa(DefaultGitConcurrency),
	"sync.default.ssh.per_hour":                   strconv.Itoa(DefaultGitSSHPerHour),
	"sync.default.https.per_hour":                 strconv.Itoa(DefaultGitHTTPSPerHour),
	"sync.default.https.per_hour_unauthenticated": strconv.Itoa(DefaultGitHTTPSUnauthenticatedPerHour),

	"sessions.retention": DefaultSessionRetention,
}

// EffectiveConfigValues computes the effective config overlay with per-key
// provenance: local wins over project wins over global, and keys no layer
// sets fall back to a baked default where one exists. Keys are emitted in
// schema declaration order; map-valued keys (api_keys) expand to one entry
// per map key with per-key provenance. Nil layers are treated as empty.
func EffectiveConfigValues(global BaseConfig, project, local *PerRepoConfig) []ConfigValue {
	return effectiveConfigValues(global, WorkConfigLayers{}, project, local, true)
}

// WorkConfigLayers are the work repository's config layers in a project_dir
// layout; nil layers are absent.
type WorkConfigLayers struct {
	Committed, Local *PerRepoConfig
	// LocalOver places Local over the project's local layer for every
	// setting, as a run starting in the work repository resolves it.
	LocalOver bool
}

// EffectiveConfigValuesWithWork is EffectiveConfigValues with a work
// repository's layers, whose hooks list between the global and the
// project's (20261109-213250-d-tac-bve).
func EffectiveConfigValuesWithWork(global BaseConfig, work WorkConfigLayers, project, local *PerRepoConfig) []ConfigValue {
	return effectiveConfigValues(global, work, project, local, true)
}

func effectiveConfigValues(global BaseConfig, work WorkConfigLayers, project, local *PerRepoConfig, mask bool) []ConfigValue {
	layers := []struct {
		cfg    *PerRepoConfig
		source ConfigValueSource
	}{
		{local, ConfigSourceLocal},
		{project, ConfigSourceProject},
		{&PerRepoConfig{BaseConfig: global}, ConfigSourceGlobal},
	}
	if work.LocalOver && work.Local != nil {
		// Its hooks list through work, apart from the project's.
		over := *work.Local
		over.Hooks = nil
		layers = slices.Insert(layers, 0, struct {
			cfg    *PerRepoConfig
			source ConfigValueSource
		}{&over, ConfigSourceWorkLocal})
	}

	provider := DefaultLLMProvider
	for _, layer := range layers {
		if layer.cfg != nil && layer.cfg.LLM.Provider != "" {
			provider = layer.cfg.LLM.Provider
			break
		}
	}

	var out []ConfigValue
	for _, leaf := range collectConfigLeaves(reflect.TypeFor[PerRepoConfig](), nil, nil) {
		switch leaf.kind {
		case leafMap:
			out = append(out, effectiveMapEntries(leaf, mask, layers)...)
		case leafConcat:
			out = append(out, concatenatedEntries(leaf, concatLayers(layers, work))...)
		default:
			set := false
			var val ConfigValue
			for _, layer := range layers {
				if layer.cfg == nil {
					continue
				}
				v := leafValue(reflect.ValueOf(*layer.cfg), leaf.index)
				if v.IsZero() {
					continue
				}
				val = ConfigValue{Key: leaf.path, Value: renderConfigValue(v), Source: layer.source}
				set = true
				break
			}
			if !set {
				def, ok := configDefaults[leaf.path]
				if leaf.path == "llm.timeout" {
					def, ok = DefaultLLMTimeoutFor(provider), true
				}
				if !ok || !providerDefault(leaf.path, provider) {
					continue
				}
				val = ConfigValue{Key: leaf.path, Value: def, Source: ConfigSourceDefault}
			}
			out = append(out, val)
		}
	}
	return out
}

type configLeafKind int

const (
	leafScalar configLeafKind = iota
	leafSlice
	leafMap
	// leafConcat is a slice whose layers MergeConfig concatenates, marked
	// `sdd:"concat"` on the field.
	leafConcat
)

type configLeaf struct {
	path  string
	index []int // field index chain from the root struct
	kind  configLeafKind
	// secret marks a field whose values must never be rendered. Declared on the
	// field with `sdd:"secret"`, never inferred from its shape: api_keys and
	// params are both maps and only one holds credentials.
	secret bool
}

// collectConfigLeaves walks a config struct type and returns its leaf fields
// in declaration order, keyed by dotted yaml-tag paths. Inline-embedded
// structs contribute their fields at the current level, matching the YAML
// shape.
func collectConfigLeaves(t reflect.Type, prefix []string, index []int) []configLeaf {
	var leaves []configLeaf
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("yaml"), ",")[0]
		inline := strings.Contains(f.Tag.Get("yaml"), ",inline")
		idx := append(append([]int{}, index...), i)
		switch {
		case f.Anonymous && inline:
			leaves = append(leaves, collectConfigLeaves(f.Type, prefix, idx)...)
		case tag == "" || tag == "-":
			continue
		case f.Type.Kind() == reflect.Struct:
			leaves = append(leaves, collectConfigLeaves(f.Type, append(append([]string{}, prefix...), tag), idx)...)
		case f.Type.Kind() == reflect.Map:
			leaves = append(leaves, configLeaf{path: joinPath(prefix, tag), index: idx, kind: leafMap, secret: hasSDDMarker(f, "secret")})
		case f.Type.Kind() == reflect.Slice && hasSDDMarker(f, "concat"):
			leaves = append(leaves, configLeaf{path: joinPath(prefix, tag), index: idx, kind: leafConcat})
		case f.Type.Kind() == reflect.Slice:
			leaves = append(leaves, configLeaf{path: joinPath(prefix, tag), index: idx, kind: leafSlice})
		default:
			leaves = append(leaves, configLeaf{path: joinPath(prefix, tag), index: idx, kind: leafScalar})
		}
	}
	return leaves
}

// hasSDDMarker reports whether a field's `sdd` tag carries marker.
func hasSDDMarker(f reflect.StructField, marker string) bool {
	for _, part := range strings.Split(f.Tag.Get("sdd"), ",") {
		if strings.TrimSpace(part) == marker {
			return true
		}
	}
	return false
}

// concatenatedEntries lists a concatenated slice's items as MergeConfig
// joins them, global first, each item keyed by its position and labelled with
// the layer that sets it. A struct item lists one entry per key it sets, by
// the yaml keys a person writes.
func concatenatedEntries(leaf configLeaf, layers []struct {
	cfg    *PerRepoConfig
	source ConfigValueSource
}) []ConfigValue {
	var out []ConfigValue
	position := 0
	for i := len(layers) - 1; i >= 0; i-- {
		layer := layers[i]
		if layer.cfg == nil {
			continue
		}
		items := leafValue(reflect.ValueOf(*layer.cfg), leaf.index)
		for j := 0; j < items.Len(); j++ {
			key := fmt.Sprintf("%s.%d", leaf.path, position)
			position++
			item := items.Index(j)
			if item.Kind() != reflect.Struct {
				out = append(out, ConfigValue{Key: key, Value: renderConfigValue(item), Source: layer.source})
				continue
			}
			for f := 0; f < item.NumField(); f++ {
				tag := strings.Split(item.Type().Field(f).Tag.Get("yaml"), ",")
				if tag[0] == "" || tag[0] == "-" || (item.Field(f).IsZero() && slices.Contains(tag[1:], "omitempty")) {
					continue
				}
				out = append(out, ConfigValue{Key: key + "." + tag[0], Value: renderConfigValue(item.Field(f)), Source: layer.source})
			}
		}
	}
	return out
}

func joinPath(prefix []string, tag string) string {
	if len(prefix) == 0 {
		return tag
	}
	return strings.Join(prefix, ".") + "." + tag
}

func leafValue(root reflect.Value, index []int) reflect.Value {
	v := root
	for _, i := range index {
		v = v.Field(i)
	}
	return v
}

// effectiveMapEntries expands a map-valued leaf into per-key entries with
// per-key provenance — the same key-by-key overlay MergeConfig applies. Only a
// leaf the field marked secret is masked.
func effectiveMapEntries(leaf configLeaf, mask bool, layers []struct {
	cfg    *PerRepoConfig
	source ConfigValueSource
}) []ConfigValue {
	type hit struct {
		source ConfigValueSource
		value  string
	}
	merged := map[string]hit{}
	var order []string
	// Walk lowest precedence first so higher layers override.
	for i := len(layers) - 1; i >= 0; i-- {
		layer := layers[i]
		if layer.cfg == nil {
			continue
		}
		v := leafValue(reflect.ValueOf(*layer.cfg), leaf.index)
		for _, k := range v.MapKeys() {
			key := k.String()
			if _, seen := merged[key]; !seen {
				order = append(order, key)
			}
			merged[key] = hit{source: layer.source, value: fmt.Sprintf("%v", v.MapIndex(k).Interface())}
		}
	}
	out := make([]ConfigValue, 0, len(order))
	for _, k := range order {
		value := "••••••"
		if !leaf.secret || !mask {
			value = merged[k].value
		}
		out = append(out, ConfigValue{
			Key:    leaf.path + "." + k,
			Value:  value,
			Source: merged[k].source,
			Secret: leaf.secret,
		})
	}
	return out
}

func renderConfigValue(v reflect.Value) string {
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() == reflect.Slice {
		parts := make([]string, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			parts = append(parts, fmt.Sprintf("%v", v.Index(i).Interface()))
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprintf("%v", v.Interface())
}

// ParseConfigScalar converts a user-typed config value into the scalar type
// it reads as: bool, int, float, or string. Mirrors YAML's own scalar
// typing so `sdd config set llm.concurrency 8` writes an int, not "8".
func ParseConfigScalar(s string) any {
	if b, err := strconv.ParseBool(s); err == nil && (s == "true" || s == "false") {
		return b
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// concatLayers places the work repository's layers between the global layer
// and the project's, keeping the local-first order of layers.
func concatLayers(layers []struct {
	cfg    *PerRepoConfig
	source ConfigValueSource
}, work WorkConfigLayers) []struct {
	cfg    *PerRepoConfig
	source ConfigValueSource
} {
	last := len(layers) - 1
	out := append(slices.Clone(layers[:last]), []struct {
		cfg    *PerRepoConfig
		source ConfigValueSource
	}{{work.Local, ConfigSourceWorkLocal}, {work.Committed, ConfigSourceWork}}...)
	return append(out, layers[last])
}

// providerDefault reports whether the baked default of path applies under
// provider: codex runs its own default model, and only the CLI providers take
// llm.effort.
func providerDefault(path, provider string) bool {
	switch path {
	case "llm.model":
		return provider != LLMProviderCodex
	case "llm.effort":
		return provider == DefaultLLMProvider || provider == LLMProviderCodex
	}
	return true
}
