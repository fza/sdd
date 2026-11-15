package cliapp_test

import (
	"strings"
	"testing"
)

const refusedRulesUpdate = "participant: Global Author\nsecrets:\n  rules_update: weekly\n"

// A user-global config whose only fault is a value sdd refuses can be
// repaired through sdd config set and unset; every other command keeps
// failing on it.
func TestConfigWritesRepairALayerCarryingARefusedValue(t *testing.T) {
	for _, repair := range [][]string{
		{"config", "set", "secrets.rules_update", "off"},
		{"config", "unset", "secrets.rules_update"},
		{"config", "--format", "json", "set", "secrets.rules_update", "daily"},
		{"--verbose", "config", "--format=json", "unset", "secrets.rules_update"},
	} {
		t.Run(strings.Join(repair, " "), func(t *testing.T) {
			f := newAppFixture(t)
			f.write(t, "config/sdd/config.yaml", refusedRulesUpdate)

			before := f.run(t, "info")
			if before.err == nil {
				t.Fatal("a command other than a config write should fail on the refused value")
			}

			repaired := f.run(t, repair...)
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
		{name: "another refused value", content: refusedRulesUpdate, args: []string{"config", "set", "secrets.rules_update", "hourly"}, want: "hourly"},
		{name: "malformed YAML", content: "secrets: [rules_update\n", args: []string{"config", "set", "secrets.rules_update", "off"}, want: "config.yaml"},
		{name: "malformed YAML on unset", content: "secrets: [rules_update\n", args: []string{"config", "unset", "secrets.rules_update"}, want: "config.yaml"},
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
	for _, args := range [][]string{{"config"}, {"config", "get", "secrets.rules_update"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := newAppFixture(t)
			f.write(t, "config/sdd/config.yaml", refusedRulesUpdate)

			result := f.run(t, args...)

			if result.err == nil || !strings.Contains(result.err.Error(), "weekly") {
				t.Fatalf("the read should fail naming the refused value: %v", result.err)
			}
		})
	}
}
