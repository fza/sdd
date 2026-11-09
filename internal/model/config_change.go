package model

import (
	"fmt"
	"strings"
)

// ConfigLayers are the config layers a process resolves its effective config
// from, plus the keys of the repository pointing at the project, which
// decide where the graph lives.
type ConfigLayers struct {
	Global  BaseConfig
	Project *PerRepoConfig
	Local   *PerRepoConfig
	Pointer *PerRepoConfig
}

// ConfigChange is one effective key whose value differs between two
// resolutions. A secret carries no values, only its key.
type ConfigChange struct {
	Key    string
	Old    string
	New    string
	Secret bool
}

// ConfigChanges lists the effective keys whose value differs from before to
// after, in schema order, followed by the pointer keys that changed.
func ConfigChanges(before, after ConfigLayers) []ConfigChange {
	old := effectiveByKey(before)
	current := effectiveByKey(after)
	var changes []ConfigChange
	seen := map[string]bool{}
	add := func(key string) {
		if seen[key] {
			return
		}
		seen[key] = true
		was, wasSet := old[key]
		now, nowSet := current[key]
		if wasSet == nowSet && was.Value == now.Value {
			return
		}
		change := ConfigChange{Key: key, Old: was.Value, New: now.Value, Secret: was.Secret || now.Secret}
		if change.Secret {
			change.Old, change.New = "", ""
		}
		changes = append(changes, change)
	}
	for _, value := range effectiveConfigValues(before.Global, before.Project, before.Local, false) {
		add(value.Key)
	}
	for _, value := range effectiveConfigValues(after.Global, after.Project, after.Local, false) {
		add(value.Key)
	}
	for _, key := range []string{"graph_repo", "project_dir"} {
		was, now := pointerValue(before.Pointer, key), pointerValue(after.Pointer, key)
		if was != now {
			changes = append(changes, ConfigChange{Key: "pointer " + key, Old: was, New: now})
		}
	}
	return changes
}

func effectiveByKey(layers ConfigLayers) map[string]ConfigValue {
	out := map[string]ConfigValue{}
	for _, value := range effectiveConfigValues(layers.Global, layers.Project, layers.Local, false) {
		out[value.Key] = value
	}
	return out
}

func pointerValue(cfg *PerRepoConfig, key string) string {
	if cfg == nil {
		return ""
	}
	if key == "graph_repo" {
		return string(cfg.GraphRepo)
	}
	return cfg.ProjectDir
}

// FormatConfigNotice renders changes as the one-time notice a serving process
// carries after its config changed; empty when nothing changed.
func FormatConfigNotice(changes []ConfigChange) string {
	if len(changes) == 0 {
		return ""
	}
	lines := make([]string, 0, len(changes)+1)
	lines = append(lines, "config changed, now in force:")
	for _, change := range changes {
		if change.Secret {
			lines = append(lines, fmt.Sprintf("- %s changed (secret, value not shown)", change.Key))
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s -> %s", change.Key, orUnset(change.Old), orUnset(change.New)))
	}
	return strings.Join(lines, "\n")
}

func orUnset(value string) string {
	if value == "" {
		return "(unset)"
	}
	return value
}
