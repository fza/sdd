package llmops

import (
	"reflect"
	"strings"
	"testing"
)

// The reflector splits a jsonschema tag at every unescaped comma, so a
// description comma must be written as `\,` or the text after it is dropped.
func TestSchemaDescriptionsReachTheSchemaWhole(t *testing.T) {
	schemas := []struct {
		name     string
		schema   map[string]any
		response any
	}{
		{name: "preflight", schema: PreflightSchema(), response: preflightResponse{}},
		{name: "writing guide", schema: WritingGuideSchema(), response: writingGuideResponse{}},
		{name: "summary", schema: SummarySchema(), response: summaryResponse{}},
		{name: "title", schema: TitleSchema(), response: titleResponse{}},
	}
	for _, tc := range schemas {
		t.Run(tc.name, func(t *testing.T) {
			assertDescriptionsMatchTags(t, tc.schema, reflect.TypeOf(tc.response))
		})
	}
}

func assertDescriptionsMatchTags(t *testing.T, schema map[string]any, structType reflect.Type) {
	t.Helper()
	properties, _ := schema["properties"].(map[string]any)
	for i := range structType.NumField() {
		field := structType.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		property, _ := properties[name].(map[string]any)
		if property == nil {
			t.Errorf("field %s should have a property in the schema", name)
			continue
		}
		if field.Type.Kind() == reflect.Slice && field.Type.Elem().Kind() == reflect.Struct {
			items, _ := property["items"].(map[string]any)
			assertDescriptionsMatchTags(t, items, field.Type.Elem())
			continue
		}
		_, written, found := strings.Cut(field.Tag.Get("jsonschema"), "description=")
		if !found {
			continue
		}
		want := strings.ReplaceAll(written, `\,`, ",")
		if got, _ := property["description"].(string); got != want {
			t.Errorf("the %s description should reach the schema as written in its tag, got %q", name, got)
		}
	}
}
