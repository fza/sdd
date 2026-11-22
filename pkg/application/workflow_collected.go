package application

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"
)

// SmallCollectedChars bounds the collected values a resume carries whole: a
// larger one goes by name with its size, and CollectedValue reads it whole
// (20261121-233256-d-tac-mm2).
const SmallCollectedChars = 300

// CollectedText renders a collected value as a serve shows it: a string as it
// is, anything else as JSON. A value that fails to encode is stamped rather
// than rendered as Go syntax, so the error shows.
func CollectedText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("[!json-encode-error: %v] %v", err, v)
	}
	return string(b)
}

// SplitCollected renders values and parts them by SmallCollectedChars: the
// small ones whole, the size in characters of each larger one. Either is nil
// when it holds nothing.
func SplitCollected(values map[string]any) (map[string]string, map[string]int) {
	var small map[string]string
	var large map[string]int
	for name, value := range values {
		text := CollectedText(value)
		if size := utf8.RuneCountInString(text); size > SmallCollectedChars {
			if large == nil {
				large = map[string]int{}
			}
			large[name] = size
			continue
		}
		if small == nil {
			small = map[string]string{}
		}
		small[name] = text
	}
	return small, large
}

// CollectedValue returns one value the session's instance collected, whole,
// as CollectedText renders it.
func (w *WorkflowSession) CollectedValue(instance, name string) (string, error) {
	inst, ok := w.session.Instance(instance)
	if !ok {
		return "", fmt.Errorf("instance %q not found in session", instance)
	}
	values := inst.Store.Collected()
	value, ok := values[name]
	if !ok {
		return "", fmt.Errorf("instance %s collected no value %q; it holds: %s", instance, name, strings.Join(slices.Sorted(maps.Keys(values)), ", "))
	}
	return CollectedText(value), nil
}
