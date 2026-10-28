package cliapp_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestInfoJSONReportsTheWriteSchemaCapabilities(t *testing.T) {
	currentCapabilities, err := model.CapabilitiesFor(model.CurrentGraphSchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name             string
		meta             string
		wantGraphSchema  any
		wantCapabilities any
	}{
		{
			name:             "graph at the binary's schema",
			meta:             fmt.Sprintf("{\n  \"graph_schema_version\": %d\n}\n", model.CurrentGraphSchemaVersion),
			wantGraphSchema:  float64(model.CurrentGraphSchemaVersion),
			wantCapabilities: map[string]any{"titles": true},
		},
		{
			name:             "graph without meta.json",
			wantGraphSchema:  nil,
			wantCapabilities: map[string]any{"titles": currentCapabilities.Titles},
		},
		{
			name:             "graph newer than the binary",
			meta:             fmt.Sprintf("{\n  \"graph_schema_version\": %d\n}\n", model.CurrentGraphSchemaVersion+1),
			wantGraphSchema:  float64(model.CurrentGraphSchemaVersion + 1),
			wantCapabilities: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t)
			if tc.meta != "" {
				f.write(t, ".sdd/meta.json", tc.meta)
			}

			info := runInfoJSON(t, f)

			if info["graph_schema"] != tc.wantGraphSchema {
				t.Errorf("graph_schema should name the graph's recorded schema, got %v", info["graph_schema"])
			}
			if info["binary_schema"] != float64(model.CurrentGraphSchemaVersion) {
				t.Errorf("binary_schema should name the binary's schema, got %v", info["binary_schema"])
			}
			if !reflect.DeepEqual(info["capabilities"], tc.wantCapabilities) {
				t.Errorf("capabilities should describe the schema writes use, got %v", info["capabilities"])
			}
			for _, key := range []string{"participant", "language", "search", "binary_schema", "capabilities", "contribution"} {
				if _, ok := info[key]; !ok {
					t.Errorf("key %s should be present on every run", key)
				}
			}
			if info["participant"] != "Local Author" || info["language"] != "de" {
				t.Errorf("participant and language should carry the session framing, got %v", info)
			}
		})
	}
}

func TestInfoRefusesAMetaWithoutSchemaVersion(t *testing.T) {
	for _, meta := range []string{"{}\n", "{\n  \"graph_schema_version\": 0\n}\n"} {
		t.Run(strings.TrimSpace(meta), func(t *testing.T) {
			f := newAppFixture(t)
			f.write(t, ".sdd/meta.json", meta)
			for _, format := range []string{"text", "json"} {
				result := f.run(t, "info", "--format", format)
				if result.err == nil {
					t.Errorf("info --format %s should refuse a meta.json the write path refuses", format)
				}
			}
		})
	}
}

func TestInfoJSONNamesEveryCapability(t *testing.T) {
	f := newAppFixture(t)
	capabilities, ok := runInfoJSON(t, f)["capabilities"].(map[string]any)
	if !ok {
		t.Fatal("capabilities should be an object")
	}
	fields := reflect.TypeFor[model.SchemaCapabilities]()
	for index := range fields.NumField() {
		field := fields.Field(index)
		key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if key == "" || key == "-" || key != strings.ToLower(key) {
			t.Errorf("SchemaCapabilities.%s should carry a lower-case JSON key", field.Name)
			continue
		}
		if _, present := capabilities[key]; !present {
			t.Errorf("capabilities should report SchemaCapabilities.%s", field.Name)
		}
	}
	if len(capabilities) != fields.NumField() {
		t.Errorf("capabilities should hold exactly one key per SchemaCapabilities field, got %v", capabilities)
	}
}

func TestInfoFormat(t *testing.T) {
	f := newAppFixture(t)

	plain := f.run(t, "info")
	text := f.run(t, "info", "--format", "text")
	if plain.err != nil || text.err != nil {
		t.Fatalf("info: %v / %v", plain.err, text.err)
	}
	if plain.stdout != text.stdout {
		t.Errorf("info without --format should print text:\n%s\nvs\n%s", plain.stdout, text.stdout)
	}
	assertContainsOutput(t, "info", plain.stdout, "Local participant: Local Author", "Language: de")

	unknown := f.run(t, "info", "--format", "yaml")
	if unknown.err == nil {
		t.Error("an unknown --format should be refused")
	}
}

func runInfoJSON(t *testing.T, f *appFixture) map[string]any {
	t.Helper()
	result := f.run(t, "info", "--format", "json")
	if result.err != nil {
		t.Fatalf("info --format json: %v\n%s", result.err, result.stderr)
	}
	var info map[string]any
	decoder := json.NewDecoder(strings.NewReader(result.stdout))
	err := decoder.Decode(&info)
	if err != nil {
		t.Fatalf("info --format json should print one JSON object: %v\n%s", err, result.stdout)
	}
	if decoder.More() {
		t.Errorf("info --format json should print exactly one JSON object:\n%s", result.stdout)
	}
	return info
}
