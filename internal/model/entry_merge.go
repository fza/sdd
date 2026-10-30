package model

import (
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// EntryMergeDriver is the git merge driver that graph entry files are routed
// to by a .gitattributes line.
const EntryMergeDriver = "sdd-entry"

// MergeResolvedKeys are the frontmatter fields sdd derives from an entry's
// body, so either side's value is valid when a merge meets two
// (20261030-085158-d-tac-vyt).
var MergeResolvedKeys = []string{"title", "summary"}

// FrontmatterField is one top-level frontmatter key as written: its decoded
// value and the raw lines stating it, key line included.
type FrontmatterField struct {
	Present bool
	Value   string
	Lines   string
}

// FrontmatterFields locates keys among the top-level keys of content's
// frontmatter. ok is false when content opens with no frontmatter parsing as
// a YAML mapping, or when one of keys holds a value that is no scalar.
func FrontmatterFields(content string, keys []string) (fields map[string]FrontmatterField, ok bool) {
	lines, regions, ok := frontmatterRegions(content)
	if !ok {
		return nil, false
	}
	fields = make(map[string]FrontmatterField, len(keys))
	for _, key := range keys {
		region, found := regions[key]
		if !found {
			fields[key] = FrontmatterField{}
			continue
		}
		if !region.scalar {
			return nil, false
		}
		fields[key] = FrontmatterField{
			Present: true,
			Value:   region.value,
			Lines:   strings.Join(lines[region.start:region.end], ""),
		}
	}
	return fields, true
}

// ResolveMergedField is the value a three-way merge keeps for a field whose
// values are all valid: a side that left the base value alone yields to the
// other, and when both changed it, or both kept it in different bytes, the
// lower one wins (absent first, then the value by byte order, then the raw
// lines), so every clone resolves the same pair alike whichever side it
// merges from.
func ResolveMergedField(base, ours, theirs FrontmatterField) FrontmatterField {
	switch {
	case ours.sameValue(base) && theirs.sameValue(base):
		if compareFields(ours, theirs) <= 0 {
			return ours
		}
		return theirs
	case ours.sameValue(base):
		return theirs
	case theirs.sameValue(base):
		return ours
	case compareFields(ours, theirs) <= 0:
		return ours
	default:
		return theirs
	}
}

// ReplaceFrontmatterField swaps the lines stating key in content's
// frontmatter for lines; empty lines remove the key. Content whose
// frontmatter lacks key, or that has none, comes back unchanged.
func ReplaceFrontmatterField(content, key, lines string) string {
	all, regions, ok := frontmatterRegions(content)
	if !ok {
		return content
	}
	region, found := regions[key]
	if !found {
		return content
	}
	return strings.Join(all[:region.start], "") + lines + strings.Join(all[region.end:], "")
}

// DropRepeatedFrontmatterKeys removes each later statement of one of keys
// whose lines repeat the first statement of it. A three-way merge of two
// sides adding the same resolved field at different lines keeps both, which
// no entry reader accepts; the first in file order stays, whichever side the
// merge ran from.
func DropRepeatedFrontmatterKeys(content string, keys []string) string {
	lines, ordered, ok := frontmatterKeyRegions(content)
	if !ok {
		return content
	}
	first := make(map[string]string, len(keys))
	drop := make([]bool, len(lines))
	for _, region := range ordered {
		if !slices.Contains(keys, region.key) {
			continue
		}
		stated := strings.Join(lines[region.start:region.end], "")
		earlier, seen := first[region.key]
		if !seen {
			first[region.key] = stated
			continue
		}
		if stated != earlier {
			continue
		}
		for i := region.start; i < region.end; i++ {
			drop[i] = true
		}
	}
	var kept strings.Builder
	for i, line := range lines {
		if !drop[i] {
			kept.WriteString(line)
		}
	}
	return kept.String()
}

func (f FrontmatterField) sameValue(other FrontmatterField) bool {
	return f.Present == other.Present && f.Value == other.Value
}

func compareFields(a, b FrontmatterField) int {
	if a.Present != b.Present {
		if a.Present {
			return 1
		}
		return -1
	}
	if c := strings.Compare(a.Value, b.Value); c != 0 {
		return c
	}
	return strings.Compare(a.Lines, b.Lines)
}

type frontmatterRegion struct {
	key        string
	start, end int
	scalar     bool
	value      string
}

// frontmatterRegions maps each top-level frontmatter key of content to the
// half-open line range stating it; a key stated twice maps to its last
// statement.
func frontmatterRegions(content string) ([]string, map[string]frontmatterRegion, bool) {
	lines, ordered, ok := frontmatterKeyRegions(content)
	if !ok {
		return nil, nil, false
	}
	regions := make(map[string]frontmatterRegion, len(ordered))
	for _, region := range ordered {
		regions[region.key] = region
	}
	return lines, regions, true
}

// frontmatterKeyRegions splits content into lines that keep their line breaks
// and lists the top-level frontmatter keys in file order, each with the
// half-open line range stating it: from its key line up to the next key, or
// to the closing delimiter.
func frontmatterKeyRegions(content string) ([]string, []frontmatterRegion, bool) {
	lines := strings.SplitAfter(content, "\n")
	if len(lines) == 0 || !isDelimiter(strings.TrimPrefix(lines[0], "\uFEFF")) {
		return nil, nil, false
	}
	closing := -1
	for i := 1; i < len(lines); i++ {
		if isDelimiter(lines[i]) {
			closing = i
			break
		}
	}
	if closing < 0 {
		return nil, nil, false
	}

	var document yaml.Node
	err := yaml.Unmarshal([]byte(strings.Join(lines[1:closing], "")), &document)
	if err != nil || len(document.Content) != 1 {
		return nil, nil, false
	}
	// A flow-style mapping states several keys on one line, so no key owns a
	// line range of its own.
	root := document.Content[0]
	if root.Kind != yaml.MappingNode || root.Style&yaml.FlowStyle != 0 {
		return nil, nil, false
	}
	mapping := root.Content

	// A YAML line number counts from the first line after the opening
	// delimiter, which is that line's index in lines.
	ordered := make([]frontmatterRegion, 0, len(mapping)/2)
	for i := 0; i+1 < len(mapping); i += 2 {
		key, value := mapping[i], mapping[i+1]
		end := closing
		if i+2 < len(mapping) {
			end = mapping[i+2].Line
		}
		ordered = append(ordered, frontmatterRegion{
			key:    key.Value,
			start:  key.Line,
			end:    end,
			scalar: value.Kind == yaml.ScalarNode,
			value:  value.Value,
		})
	}
	return lines, ordered, true
}

func isDelimiter(line string) bool {
	return strings.TrimRight(line, " \t\r\n") == "---"
}
