package model

import (
	"fmt"
	"slices"
	"strings"
)

// ClosedValues is a config setting type whose values form a fixed set,
// listed loosest first where the set is ordered.
type ClosedValues interface {
	AllowedValues() []string
}

func checkClosedValue(key, value string, allowed []string) error {
	if slices.Contains(allowed, value) {
		return nil
	}
	return fmt.Errorf("%s %q is not %s", key, value, oneOf(allowed))
}

func closedValues[S ~string](values ...S) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

// oneOf renders values as "a, b or c".
func oneOf(values []string) string {
	if len(values) < 2 {
		return strings.Join(values, "")
	}
	return strings.Join(values[:len(values)-1], ", ") + " or " + values[len(values)-1]
}
