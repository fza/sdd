package model

import (
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// StagedHeaderKeys are the header keys of a staged body file that are checked
// against the capture report field of the same name.
var StagedHeaderKeys = []string{"entryKind", "layer", "confidence", "intent", "refs", "topics", "closes", "supersedes"}

// SplitStagedBody splits a staged body file into its header and its body. A
// file whose first line is `---` opens with a header block when a second such
// line closes a YAML mapping holding one of StagedHeaderKeys; the body is then
// everything after the second line. Any other file is all body, so a markdown
// body opening with a `---` rule keeps its first section. A leading byte order
// mark never reaches the body. header is nil when there is no block.
//
// A header block holding a line that opens with `#` at the margin is
// ambiguous: a YAML comment, but also a markdown heading. Such a file is all
// body and ambiguous is true, for the confirm to refuse.
func SplitStagedBody(content string) (header map[string]any, body string, ambiguous bool) {
	content = strings.TrimPrefix(content, "\uFEFF")
	lines := strings.SplitAfter(content, "\n")
	if !isHeaderDelimiter(lines[0]) {
		return nil, content, false
	}
	for i := 1; i < len(lines); i++ {
		if !isHeaderDelimiter(lines[i]) {
			continue
		}
		var parsed map[string]any
		if err := yaml.Unmarshal([]byte(strings.Join(lines[1:i], "")), &parsed); err != nil {
			return nil, content, false
		}
		if !slices.ContainsFunc(StagedHeaderKeys, func(key string) bool { _, ok := parsed[key]; return ok }) {
			return nil, content, false
		}
		if slices.ContainsFunc(lines[1:i], func(line string) bool { return strings.HasPrefix(line, "#") }) {
			return nil, content, true
		}
		return parsed, strings.Join(lines[i+1:], ""), false
	}
	return nil, content, false
}

// StagedHeaderMismatch names the first of StagedHeaderKeys whose header value
// has the shape of the report field of that name but states something else.
// report holds the capture's fields as the engine stores them. A key the
// report does not state, or whose header value has another shape, is not
// compared.
func StagedHeaderMismatch(header, report map[string]any) (string, bool) {
	for _, key := range StagedHeaderKeys {
		stated, inHeader := header[key]
		reported, inReport := report[key]
		if !inHeader || !inReport {
			continue
		}
		want, ok := stagedFieldForm(key, reported)
		if !ok {
			continue
		}
		got, ok := stagedFieldForm(key, stated)
		if !ok {
			continue
		}
		if !slices.Equal(got, want) {
			return key, true
		}
	}
	return "", false
}

// stagedFieldForm is value in the comparable form of the field key: one
// element for a scalar, the sorted elements of a list. ok is false when value
// has another shape than the field takes.
func stagedFieldForm(key string, value any) ([]string, bool) {
	switch key {
	case "refs":
		items, ok := value.([]any)
		if !ok {
			return nil, false
		}
		form := make([]string, 0, len(items))
		for _, item := range items {
			ref, ok := stagedRefForm(item)
			if !ok {
				return nil, false
			}
			form = append(form, ref)
		}
		slices.Sort(form)
		return form, true
	case "topics", "closes", "supersedes":
		items, ok := value.([]any)
		if !ok {
			return nil, false
		}
		form := make([]string, 0, len(items))
		for _, item := range items {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			if key != "topics" {
				if _, err := ParseID(text); err != nil {
					return nil, false
				}
			}
			form = append(form, text)
		}
		slices.Sort(form)
		return form, true
	case "layer":
		text, ok := value.(string)
		if !ok {
			return nil, false
		}
		layer, ok := ParseLayerFilter(text)
		if !ok {
			return []string{text}, true
		}
		return []string{string(layer)}, true
	default:
		text, ok := value.(string)
		if !ok {
			return nil, false
		}
		return []string{text}, true
	}
}

// stagedRefForm is one ref as a comparable string; ok is false unless item is
// a mapping of id and kind, with desc optional and no other key.
func stagedRefForm(item any) (string, bool) {
	ref, ok := item.(map[string]any)
	if !ok {
		return "", false
	}
	var parts [3]string
	for name, value := range ref {
		text, isText := value.(string)
		index := slices.Index([]string{"id", "kind", "desc"}, name)
		if !isText || index < 0 {
			return "", false
		}
		parts[index] = text
	}
	if parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return strings.Join(parts[:], "\x00"), true
}

func isHeaderDelimiter(line string) bool {
	return strings.TrimRight(line, "\r\n") == "---"
}
