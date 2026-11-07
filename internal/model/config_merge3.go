package model

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// MergeNoticePrefix opens every line a merge driver writes for sdd to relay
// as a notice.
const MergeNoticePrefix = "sdd notice: "

// MergeConfigYAML three-way merges `.sdd/config.yaml` per key for the
// sdd-config driver (20261106-202055-d-tac-eyl): a key changed on one side is
// taken, `dependencies` merges as a set of both sides' additions and
// removals, and a value changed on both sides takes theirs, the upstream,
// with a notice naming the key. The result is theirs with those changes
// applied, so comments and layout follow upstream.
func MergeConfigYAML(base, ours, theirs []byte) (merged []byte, notices []string, err error) {
	var roots [3]*yaml.Node
	for i, data := range [][]byte{base, ours, theirs} {
		roots[i], err = parseYAMLRoot(data)
		if err != nil {
			return nil, nil, fmt.Errorf("reading the %s version: %w", [3]string{"base", "our", "their"}[i], err)
		}
		if roots[i].Content[0].Kind == yaml.ScalarNode && roots[i].Content[0].Tag == "!!null" {
			roots[i].Content[0] = &yaml.Node{Kind: yaml.MappingNode}
		}
		if roots[i].Content[0].Kind != yaml.MappingNode {
			return nil, nil, fmt.Errorf("the %s version is not a mapping", [3]string{"base", "our", "their"}[i])
		}
	}
	merger := configMerger{}
	merger.mapping(nil, roots[0].Content[0], roots[1].Content[0], roots[2].Content[0])
	out, err := encodeYAML(roots[2])
	if err != nil {
		return nil, nil, err
	}
	if len(bytes.TrimSpace(out)) == 0 || string(bytes.TrimSpace(out)) == "{}" {
		out = nil
	}
	return out, merger.notices, nil
}

type configMerger struct {
	notices []string
}

func (m *configMerger) mapping(prefix []string, base, ours, result *yaml.Node) {
	for _, key := range mappingKeys(base, ours, result) {
		path := append(slices.Clone(prefix), key)
		baseValue, ourValue, theirValue := mappingValue(base, key), mappingValue(ours, key), mappingValue(result, key)
		if isMapping(ourValue) && isMapping(theirValue) && (baseValue == nil || isMapping(baseValue)) {
			if baseValue == nil {
				baseValue = &yaml.Node{Kind: yaml.MappingNode}
			}
			m.mapping(path, baseValue, ourValue, theirValue)
			continue
		}
		if len(path) == 1 && key == "dependencies" {
			m.dependencies(result, baseValue, ourValue, theirValue)
			continue
		}
		oursChanged := !sameYAML(baseValue, ourValue)
		theirsChanged := !sameYAML(baseValue, theirValue)
		switch {
		case !oursChanged:
		case !theirsChanged:
			setMappingValue(result, key, ourValue)
		case !sameYAML(ourValue, theirValue):
			m.notices = append(m.notices, fmt.Sprintf("config.yaml: %s changed on both sides; took the upstream value", strings.Join(path, ".")))
		}
	}
}

func (m *configMerger) dependencies(result, base, ours, theirs *yaml.Node) {
	baseSet, ourSet, theirSet := scalarItems(base), scalarItems(ours), scalarItems(theirs)
	merged := slices.Clone(theirSet)
	for _, item := range ourSet {
		if !slices.Contains(baseSet, item) && !slices.Contains(merged, item) {
			merged = append(merged, item)
		}
	}
	merged = slices.DeleteFunc(merged, func(item string) bool {
		return slices.Contains(baseSet, item) && !slices.Contains(ourSet, item)
	})
	if slices.Equal(merged, theirSet) {
		return
	}
	if len(merged) == 0 {
		setMappingValue(result, "dependencies", nil)
		return
	}
	style := yaml.Style(0)
	if theirs != nil {
		style = theirs.Style
	} else if ours != nil {
		style = ours.Style
	}
	items := make([]*yaml.Node, 0, len(merged))
	for _, item := range merged {
		items = append(items, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: item})
	}
	sequence := &yaml.Node{Kind: yaml.SequenceNode, Style: style, Content: items}
	if theirs != nil {
		replaceNodeValue(theirs, sequence)
		return
	}
	setMappingValue(result, "dependencies", sequence)
}

func mappingKeys(nodes ...*yaml.Node) []string {
	var keys []string
	for _, node := range nodes {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if !slices.Contains(keys, node.Content[i].Value) {
				keys = append(keys, node.Content[i].Value)
			}
		}
	}
	return keys
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// setMappingValue replaces key's value in node, adds it when missing, and
// removes the key for a nil value.
func setMappingValue(node *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value != key {
			continue
		}
		if value == nil {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
		node.Content[i+1] = value
		return
	}
	if value != nil {
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
	}
}

func isMapping(node *yaml.Node) bool { return node != nil && node.Kind == yaml.MappingNode }

func scalarItems(node *yaml.Node) []string {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	items := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		items = append(items, item.Value)
	}
	return items
}

// sameYAML compares two values by content, ignoring comments and style.
func sameYAML(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	var left, right any
	errLeft := a.Decode(&left)
	errRight := b.Decode(&right)
	if errLeft != nil || errRight != nil {
		return false
	}
	return fmt.Sprintf("%#v", left) == fmt.Sprintf("%#v", right)
}
