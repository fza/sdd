package cliapp_test

import (
	"strings"
	"testing"
)

// A config layer whose only fault is a value sdd refuses can be repaired
// through sdd config set and unset; every other command keeps failing on it.
func TestConfigWritesRepairALayerCarryingARefusedValue(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content string
		repair  []string
	}{
		{name: "dialogue.mode set globally", file: "config/sdd/config.yaml", content: "participant: Global Author\ndialogue:\n  mode: autonomous\n", repair: []string{"config", "set", "dialogue.mode", "auto"}},
		{name: "dialogue.mode unset globally", file: "config/sdd/config.yaml", content: "participant: Global Author\ndialogue:\n  mode: autonomous\n", repair: []string{"config", "unset", "dialogue.mode"}},
		{name: "secrets.rules_update set globally", file: "config/sdd/config.yaml", content: "participant: Global Author\nsecrets:\n  rules_update: weekly\n", repair: []string{"config", "set", "secrets.rules_update", "off"}},
		{name: "dialogue.mode set locally", file: ".sdd/config.local.yaml", content: "participant: Local Author\ndialogue:\n  mode: autonomous\n", repair: []string{"config", "set", "--local", "dialogue.mode", "auto"}},
		{name: "dialogue.mode unset locally", file: ".sdd/config.local.yaml", content: "participant: Local Author\ndialogue:\n  mode: autonomous\n", repair: []string{"config", "unset", "--local", "dialogue.mode"}},
		{name: "dialogue.mode set after a config flag", file: "config/sdd/config.yaml", content: "participant: Global Author\ndialogue:\n  mode: autonomous\n", repair: []string{"config", "--format", "json", "set", "dialogue.mode", "auto"}},
		{name: "dialogue.mode unset after a root flag", file: "config/sdd/config.yaml", content: "participant: Global Author\ndialogue:\n  mode: autonomous\n", repair: []string{"--verbose", "config", "--format=json", "unset", "dialogue.mode"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t)
			f.write(t, tc.file, tc.content)

			before := f.run(t, "info")
			if before.err == nil {
				t.Fatal("a command other than a config write should fail on the refused value")
			}

			repaired := f.run(t, tc.repair...)
			if repaired.err != nil {
				t.Fatalf("the config write should repair the layer: %v\n%s", repaired.err, repaired.stderr)
			}

			after := f.run(t, "info")
			if after.err != nil {
				t.Fatalf("the next command should load the repaired config: %v", after.err)
			}
		})
	}
}

func TestConfigSetStillRefusesOnABrokenLayer(t *testing.T) {
	cases := []struct {
		name    string
		content string
		args    []string
		want    string
	}{
		{name: "another refused value", content: "dialogue:\n  mode: autonomous\n", args: []string{"config", "set", "dialogue.mode", "bogus"}, want: "bogus"},
		{name: "malformed YAML", content: "dialogue: [mode\n", args: []string{"config", "set", "dialogue.mode", "auto"}, want: "config.yaml"},
		{name: "malformed YAML on unset", content: "dialogue: [mode\n", args: []string{"config", "unset", "dialogue.mode"}, want: "config.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t)
			f.write(t, "config/sdd/config.yaml", tc.content)

			result := f.run(t, tc.args...)

			if result.err == nil {
				t.Fatalf("the config write should fail, got stdout %q", result.stdout)
			}
			if !strings.Contains(result.err.Error(), tc.want) {
				t.Errorf("the error should name the fault, missing %q: %v", tc.want, result.err)
			}
		})
	}
}

// Only a config write is let past the refused value: reading the config is
// not a repair.
func TestConfigReadsFailOnARefusedValue(t *testing.T) {
	for _, args := range [][]string{{"config"}, {"config", "get", "dialogue.mode"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := newAppFixture(t)
			f.write(t, "config/sdd/config.yaml", "dialogue:\n  mode: autonomous\n")

			result := f.run(t, args...)

			if result.err == nil || !strings.Contains(result.err.Error(), "autonomous") {
				t.Fatalf("the read should fail naming the refused value: %v", result.err)
			}
		})
	}
}
