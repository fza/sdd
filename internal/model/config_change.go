package model

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// ConfigLayers are the config layers a process resolves its effective config
// from.
type ConfigLayers struct {
	Global  BaseConfig
	Project *PerRepoConfig
	Local   *PerRepoConfig
}

func (l ConfigLayers) values() []ConfigValue {
	return effectiveConfigValues(l.Global, l.Project, l.Local, false)
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
// after, in schema order. Secrets are compared by value and reported by key
// only.
func ConfigChanges(before, after ConfigLayers) []ConfigChange {
	oldValues, newValues := before.values(), after.values()
	old := configValuesByKey(oldValues)
	current := configValuesByKey(newValues)
	var keys []string
	for _, value := range slices.Concat(oldValues, newValues) {
		if !slices.Contains(keys, value.Key) {
			keys = append(keys, value.Key)
		}
	}
	leaves := collectConfigLeaves(reflect.TypeFor[PerRepoConfig](), nil, nil)
	slices.SortStableFunc(keys, func(a, b string) int { return leafRank(leaves, a) - leafRank(leaves, b) })

	var changes []ConfigChange
	for _, key := range keys {
		was, wasSet := old[key]
		now, nowSet := current[key]
		if wasSet == nowSet && was.Value == now.Value {
			continue
		}
		change := ConfigChange{Key: key, Old: was.Value, New: now.Value, Secret: was.Secret || now.Secret}
		if change.Secret {
			change.Old, change.New = "", ""
		}
		changes = append(changes, change)
	}
	return changes
}

// leafRank is the schema position of the leaf an effective key belongs to.
func leafRank(leaves []configLeaf, key string) int {
	for i, leaf := range leaves {
		if key == leaf.path || leaf.kind == leafMap && strings.HasPrefix(key, leaf.path+".") {
			return i
		}
	}
	return len(leaves)
}

func configValuesByKey(values []ConfigValue) map[string]ConfigValue {
	out := make(map[string]ConfigValue, len(values))
	for _, value := range values {
		out[value.Key] = value
	}
	return out
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
