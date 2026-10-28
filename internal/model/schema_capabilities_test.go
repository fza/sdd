package model_test

import (
	"errors"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestCapabilitiesForEveryKnownVersion(t *testing.T) {
	for version := 1; version <= model.CurrentGraphSchemaVersion; version++ {
		_, err := model.CapabilitiesFor(version)
		if err != nil {
			t.Errorf("every version up to the binary's should state its capabilities: %v", err)
		}
	}
	current, err := model.CapabilitiesFor(model.CurrentGraphSchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !current.Titles {
		t.Error("the binary's own schema should carry titles")
	}
}

func TestCapabilitiesForUnknownVersion(t *testing.T) {
	for _, version := range []int{0, model.CurrentGraphSchemaVersion + 1} {
		_, err := model.CapabilitiesFor(version)
		if !errors.Is(err, model.ErrUnknownSchemaVersion) {
			t.Errorf("a version the binary does not know should be refused as unknown, version %d gave %v", version, err)
		}
	}
}

func TestParseSchemaMetaRefusesAMissingVersion(t *testing.T) {
	cases := []struct {
		name   string
		data   string
		refuse bool
	}{
		{name: "key missing", data: "{}", refuse: true},
		{name: "zero", data: `{"graph_schema_version": 0}`, refuse: true},
		{name: "negative", data: `{"graph_schema_version": -1}`, refuse: true},
		{name: "first schema", data: `{"graph_schema_version": 1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := model.ParseSchemaMeta([]byte(tc.data))
			if tc.refuse && err == nil {
				t.Error("a meta.json without a valid schema version should be refused")
			}
			if !tc.refuse && err != nil {
				t.Errorf("a valid schema version should parse: %v", err)
			}
		})
	}
}

func TestWriteSchemaVersion(t *testing.T) {
	if model.WriteSchemaVersion(nil) != model.CurrentGraphSchemaVersion {
		t.Error("a graph without meta.json should be written at the binary's schema")
	}
	if model.WriteSchemaVersion(&model.SchemaMeta{GraphSchemaVersion: 1}) != 1 {
		t.Error("a graph with meta.json should be written at its recorded schema")
	}
}

func TestCheckCompatibilityAcceptsAnOlderGraph(t *testing.T) {
	floor := "v0.3.0"
	cases := []struct {
		name       string
		meta       model.SchemaMeta
		binary     string
		compatible bool
	}{
		{name: "older graph", meta: model.SchemaMeta{GraphSchemaVersion: 1}, binary: "v1.0.0", compatible: true},
		{name: "same graph", meta: model.SchemaMeta{GraphSchemaVersion: 2}, binary: "v1.0.0", compatible: true},
		{name: "newer graph", meta: model.SchemaMeta{GraphSchemaVersion: 3}, binary: "v1.0.0", compatible: false},
		{name: "older graph below its floor", meta: model.SchemaMeta{GraphSchemaVersion: 1, MinimumVersion: &floor}, binary: "v0.2.0", compatible: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := model.CheckCompatibility(tc.meta, tc.binary, 2)
			if got.Compatible != tc.compatible {
				t.Errorf("compatibility should follow the schema rules, got %+v", got)
			}
		})
	}
}
