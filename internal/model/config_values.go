package model

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// ClosedValues is a config setting type whose values form a fixed set. Loading
// refuses or reads past any other value, so `sdd config set` refuses it before
// it lands (20261110-015917-d-tac-yc6).
type ClosedValues interface {
	AllowedValues() []string
}

// FreeText marks a named string config setting type whose values form no
// fixed set.
type FreeText interface {
	FreeText()
}

// ConfigKeyType returns the type of the setting segments names in schema t:
// inline-embedded structs contribute their fields at their own level, a
// pointer stands for its element and a map consumes one segment as its key.
func ConfigKeyType(t reflect.Type, segments []string) (reflect.Type, bool) {
	for len(segments) > 0 {
		switch t.Kind() {
		case reflect.Pointer:
			t = t.Elem()
		case reflect.Map:
			t, segments = t.Elem(), segments[1:]
		case reflect.Struct:
			field, ok := yamlField(t, segments[0])
			if !ok {
				return nil, false
			}
			t, segments = field.Type, segments[1:]
		default:
			return nil, false
		}
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t, true
}

func yamlField(t reflect.Type, name string) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("yaml")
		if f.Anonymous && strings.Contains(tag, ",inline") {
			if inner, ok := yamlField(f.Type, name); ok {
				return inner, true
			}
			continue
		}
		if strings.Split(tag, ",")[0] == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

// ConfigValueRefusal refuses value for the setting segments names in schema
// t: an empty value, since `sdd config unset` removes a key, or a value
// outside the set the setting's type allows.
func ConfigValueRefusal(t reflect.Type, segments []string, value string) error {
	key := strings.Join(segments, ".")
	if value == "" {
		return fmt.Errorf("%s takes no empty value; `sdd config unset` removes a key", key)
	}
	typ, ok := ConfigKeyType(t, segments)
	if !ok {
		return nil
	}
	closed, ok := reflect.Zero(typ).Interface().(ClosedValues)
	if !ok {
		return nil
	}
	return checkClosedValue(key, value, closed.AllowedValues())
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
