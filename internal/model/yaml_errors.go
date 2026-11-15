package model

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	quotedValue = regexp.MustCompile("`[^`]*`")
	errorLine   = regexp.MustCompile(`^line (\d+): `)
)

// withoutValues rewrites a YAML type error to name the key and line of each
// mismatch instead of the value: a value in the wrong place may be a secret,
// and the error reaches agents and logs.
func withoutValues(data []byte, err error) error {
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return err
	}
	keys := keysByLine(data)
	messages := make([]string, 0, len(typeErr.Errors))
	for _, message := range typeErr.Errors {
		message = strings.Join(strings.Fields(quotedValue.ReplaceAllString(message, "")), " ")
		match := errorLine.FindStringSubmatch(message)
		if match != nil {
			line, _ := strconv.Atoi(match[1])
			if key, ok := keys[line]; ok {
				message = fmt.Sprintf("line %d: %s: %s", line, key, strings.TrimPrefix(message, match[0]))
			}
		}
		messages = append(messages, message)
	}
	return fmt.Errorf("yaml: unmarshal errors:\n  %s", strings.Join(messages, "\n  "))
}

// keysByLine maps each line a mapping value starts on to its dotted key, the
// innermost key where several share a line.
func keysByLine(data []byte) map[int]string {
	keys := map[int]string{}
	var root yaml.Node
	if yaml.Unmarshal(data, &root) != nil {
		return keys
	}
	var walk func(node *yaml.Node, path string)
	walk = func(node *yaml.Node, path string) {
		switch node.Kind {
		case yaml.DocumentNode, yaml.SequenceNode:
			for _, child := range node.Content {
				walk(child, path)
			}
		case yaml.MappingNode:
			for i := 0; i+1 < len(node.Content); i += 2 {
				key := node.Content[i].Value
				if path != "" {
					key = path + "." + key
				}
				keys[node.Content[i+1].Line] = key
				walk(node.Content[i+1], key)
			}
		}
	}
	walk(&root, "")
	return keys
}
