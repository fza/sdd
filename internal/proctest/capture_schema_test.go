// The capture's report schemas carry only the fields a report fills there.
package proctest_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
)

func TestCaptureSchemasCarryOnlyFieldsAReportFills(t *testing.T) {
	_, session := newCaptureWorld(t, "capture-schema")
	serve := session.Start(t, "capture", nil)
	proctest.RequireStep(t, serve, "assemble")
	props, _ := serve.ReportSchema["properties"].(map[string]any)
	for _, absent := range []string{"deepFindings", "deepResponses", "lateVerdicts", "deepLaunchFailure", "fidelityNote", "procedureSpec"} {
		if _, ok := props[absent]; ok {
			t.Errorf("the assemble schema should leave out %s", absent)
		}
	}
	if _, ok := props["body"]; !ok {
		t.Fatal("the assemble schema should carry the draft fields")
	}

	serve = session.Start(t, "capture", map[string]any{"kind": "procedure"})
	props, _ = serve.ReportSchema["properties"].(map[string]any)
	if _, ok := props["procedureSpec"]; !ok {
		t.Error("a procedure capture's assemble schema should carry procedureSpec")
	}
}

// A report schema's descriptions name a field's shape and constraint only:
// the step's lanes carry the guidance (20261112-075604-d-tac-t4r).
func TestCaptureSchemaDescriptionsStayShortAndKeepTheirConstraints(t *testing.T) {
	const maxDescription = 90
	_, session := newCaptureWorld(t, "capture-schema-descriptions")
	serve := session.Start(t, "capture", nil)
	proctest.RequireStep(t, serve, "assemble")
	descriptions := schemaDescriptions("", serve.ReportSchema)
	for path, description := range descriptions {
		if len(description) > maxDescription {
			t.Errorf("%s should describe its shape and constraint in at most %d characters, got %d: %q", path, maxDescription, len(description), description)
		}
	}
	for path, constraint := range map[string]string{
		"intent":              "required when entryKind is directive",
		"actorKind":           "never human",
		"bodyPatch":           "a failing pair refuses the report",
		"participants":        "empty writes the local participant",
		"index":               "also in topics",
		"bodyPatch.items.old": "exactly once",
	} {
		if !strings.Contains(descriptions[path], constraint) {
			t.Errorf("%s should keep its constraint %q, got %q", path, constraint, descriptions[path])
		}
	}
}

// schemaDescriptions maps each description in schema to its property path.
func schemaDescriptions(path string, schema map[string]any) map[string]string {
	out := map[string]string{}
	if description, ok := schema["description"].(string); ok {
		out[path] = description
	}
	join := func(name string) string {
		if path == "" {
			return name
		}
		return path + "." + name
	}
	if props, ok := schema["properties"].(map[string]any); ok {
		for name, prop := range props {
			if node, ok := prop.(map[string]any); ok {
				maps.Copy(out, schemaDescriptions(join(name), node))
			}
		}
	}
	if items, ok := schema["items"].(map[string]any); ok {
		maps.Copy(out, schemaDescriptions(join("items"), items))
	}
	if anyOf, ok := schema["anyOf"].([]any); ok {
		for _, branch := range anyOf {
			if node, ok := branch.(map[string]any); ok {
				for p, d := range schemaDescriptions(path, node) {
					if _, set := out[p]; !set {
						out[p] = d
					}
				}
			}
		}
	}
	return out
}

// An assemble report missing fields is served again with the missing fields'
// schema alone; a complete report then plays the draft back.
func TestCaptureHeldAssembleServesTheMissingFieldsSchema(t *testing.T) {
	_, session := newCaptureWorld(t, "capture-failing-schema")
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	partial := captureDraft()
	delete(partial, "layer")
	delete(partial, "entryKind")
	serve = session.Report(t, instance, partial)
	proctest.RequireStep(t, serve, "assemble")
	props, _ := serve.ReportSchema["properties"].(map[string]any)
	if len(props) != 2 || props["layer"] == nil || props["entryKind"] == nil {
		t.Fatalf("the held assemble should serve the missing fields alone, got %v", slices.Sorted(maps.Keys(props)))
	}
	if required, _ := serve.ReportSchema["required"].([]string); !slices.Equal(required, []string{"entryKind", "layer"}) {
		t.Errorf("the missing fields should stay required, got %v", required)
	}
	serve = session.Report(t, instance, map[string]any{"layer": "tactical", "entryKind": "gap"})
	proctest.RequireStep(t, serve, "playback")
}
