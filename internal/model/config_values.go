package model

import (
	"fmt"
	"reflect"
	"strings"
)

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
	// llm.provider is a plain string field, so its closed set is named here.
	if key == "llm.provider" {
		return checkClosedValue(key, value, LLMProviders())
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
