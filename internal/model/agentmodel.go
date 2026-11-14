package model

import "strings"

// HoldNextModel reports whether an agent running on model, as its harness
// names it, gets the hold-next line (20261114-121534-d-tac-qdb): the name
// holds "opus" or "fable", matched case-insensitively. An empty model, one
// not reported, gets none.
func HoldNextModel(model string) bool {
	name := strings.ToLower(model)
	return strings.Contains(name, "opus") || strings.Contains(name, "fable")
}
