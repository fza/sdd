package model_test

import (
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
	first, err := model.CapabilitiesFor(1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Titles {
		t.Error("schema 1 should not carry titles")
	}
}

func TestCapabilitiesForUnknownVersion(t *testing.T) {
	for _, version := range []int{0, model.CurrentGraphSchemaVersion + 1} {
		_, err := model.CapabilitiesFor(version)
		if err == nil {
			t.Errorf("a version the binary does not know should be refused, version %d passed", version)
		}
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
