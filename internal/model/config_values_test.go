package model_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
)

// Every named string type a config schema holds says whether its values form
// a fixed set, so `sdd config set` can refuse what loading would refuse
// (20261110-015917-d-tac-yc6).
func TestConfigSchemasDeclareTheValuesOfEveryNamedStringType(t *testing.T) {
	closed := reflect.TypeFor[model.ClosedValues]()
	free := reflect.TypeFor[model.FreeText]()
	for _, schema := range []reflect.Type{
		reflect.TypeFor[model.BaseConfig](),
		reflect.TypeFor[model.PerRepoConfig](),
		reflect.TypeFor[repos.GlobalConfig](),
	} {
		for path, typ := range namedStringTypes(schema, schema.Name(), map[reflect.Type]bool{}) {
			t.Run(path, func(t *testing.T) {
				isClosed, isFree := typ.Implements(closed), typ.Implements(free)
				if isClosed == isFree {
					t.Errorf("%s should declare either AllowedValues or FreeText", typ)
				}
				if isClosed && len(reflect.Zero(typ).Interface().(model.ClosedValues).AllowedValues()) == 0 {
					t.Errorf("%s should allow at least one value", typ)
				}
			})
		}
	}
}

func namedStringTypes(t reflect.Type, path string, seen map[reflect.Type]bool) map[string]reflect.Type {
	found := map[string]reflect.Type{}
	add := func(more map[string]reflect.Type) {
		for k, v := range more {
			found[k] = v
		}
	}
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice:
		add(namedStringTypes(t.Elem(), path, seen))
	case reflect.Map:
		add(namedStringTypes(t.Key(), path+".<key>", seen))
		add(namedStringTypes(t.Elem(), path+".*", seen))
	case reflect.String:
		if t.PkgPath() != "" {
			found[path] = t
		}
	case reflect.Struct:
		if seen[t] {
			return found
		}
		seen[t] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := strings.Split(f.Tag.Get("yaml"), ",")[0]
			if name == "-" || (name == "" && !f.Anonymous) {
				continue
			}
			add(namedStringTypes(f.Type, strings.TrimSuffix(path+"."+name, "."), seen))
		}
		delete(seen, t)
	}
	return found
}

func TestConfigValueRefusal(t *testing.T) {
	schema := reflect.TypeFor[model.PerRepoConfig]()
	cases := []struct {
		name     string
		segments []string
		value    string
		refusal  string
	}{
		{name: "allowed value", segments: []string{"verify", "mode"}, value: "deep"},
		{name: "value outside the set", segments: []string{"verify", "mode"}, value: "light", refusal: `verify.mode "light" is not off, grounding, prose or deep`},
		{name: "value below a map", segments: []string{"events", "channels", "recheck", "share"}, value: "s3", refusal: `"s3" is not local or git`},
		{name: "value below an inline block", segments: []string{"llm_prefilter", "structured_output"}, value: "xml", refusal: "json_schema, json_object or auto"},
		{name: "free text", segments: []string{"repo_id"}, value: "example.com/a/b"},
		{name: "plain string", segments: []string{"llm", "model"}, value: "anything"},
		{name: "empty value", segments: []string{"participant"}, value: "", refusal: "participant takes no empty value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := model.ConfigValueRefusal(schema, tc.segments, tc.value)
			if tc.refusal == "" {
				if err != nil {
					t.Errorf("the value should be accepted: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.refusal) {
				t.Errorf("the value should be refused naming the allowed values, got %v", err)
			}
		})
	}
}
