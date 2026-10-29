package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestDialogueModeAtConfigLoad(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want model.DialogueMode
	}{
		{name: "unset", yaml: "participant: Ada\n", want: model.DialogueGuided},
		{name: "guided", yaml: "dialogue:\n  mode: guided\n", want: model.DialogueGuided},
		{name: "autonomous", yaml: "dialogue:\n  mode: autonomous\n", want: model.DialogueAutonomous},
		{name: "unknown value", yaml: "dialogue:\n  mode: free\n", want: model.DialogueGuided},
		{name: "wrong case", yaml: "dialogue:\n  mode: Autonomous\n", want: model.DialogueGuided},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := model.ParseConfig([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("any mode value should load: %v", err)
			}
			if got := model.ResolveDialogueMode(cfg); got != tc.want {
				t.Errorf("only autonomous should hand choices to the agent, got %q", got)
			}
		})
	}
}

func TestIgnoredDialogueMode(t *testing.T) {
	cases := []struct {
		name      string
		mode      model.DialogueMode
		committed bool
		ignored   bool
	}{
		{name: "unset in the committed layer", committed: true},
		{name: "autonomous in the committed layer", mode: model.DialogueAutonomous, committed: true, ignored: true},
		{name: "guided in the committed layer", mode: model.DialogueGuided, committed: true, ignored: true},
		{name: "autonomous in a personal layer", mode: model.DialogueAutonomous},
		{name: "guided in a personal layer", mode: model.DialogueGuided},
		{name: "unknown value in a personal layer", mode: "free", ignored: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := model.IgnoredSettings(model.BaseConfig{Dialogue: model.DialogueConfig{Mode: tc.mode}}, tc.committed)
			if !tc.ignored {
				if len(got) != 0 {
					t.Errorf("a setting the layer may make should be honored, got %v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Key != "dialogue.mode" || got[0].Reason == "" {
				t.Errorf("the setting should be reported as ignored with its key and a reason, got %v", got)
			}
		})
	}
}

func TestHonoredCommittedDropsTheDialogueMode(t *testing.T) {
	committed := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Dialogue: model.DialogueConfig{Mode: model.DialogueAutonomous}}, GraphDir: "graph"}
	honored := model.HonoredCommitted(committed)
	if honored.Dialogue.Mode != "" || honored.GraphDir != "graph" {
		t.Errorf("only the dialogue mode should be dropped from the committed layer, got %+v", honored)
	}
	if committed.Dialogue.Mode != model.DialogueAutonomous {
		t.Error("the layer read should be left as it was")
	}
}

func TestDialogueModeLayering(t *testing.T) {
	autonomous := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Dialogue: model.DialogueConfig{Mode: model.DialogueAutonomous}}}
	guided := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Dialogue: model.DialogueConfig{Mode: model.DialogueGuided}}}
	cases := []struct {
		name    string
		base    *model.PerRepoConfig
		overlay *model.PerRepoConfig
		want    model.DialogueMode
	}{
		{name: "higher layer overrides", base: autonomous, overlay: guided, want: model.DialogueGuided},
		{name: "higher layer turns it on", base: guided, overlay: autonomous, want: model.DialogueAutonomous},
		{name: "unset higher layer keeps lower", base: autonomous, overlay: &model.PerRepoConfig{}, want: model.DialogueAutonomous},
		{name: "unset everywhere", base: &model.PerRepoConfig{}, overlay: &model.PerRepoConfig{}, want: model.DialogueGuided},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.ResolveDialogueMode(model.MergeConfig(tc.base, tc.overlay)); got != tc.want {
				t.Errorf("the merged mode should come from the highest layer setting it, got %q", got)
			}
		})
	}
}

func TestDialogueModeEffectiveDefault(t *testing.T) {
	for _, v := range model.EffectiveConfigValues(model.BaseConfig{}, nil, nil) {
		if v.Key == "dialogue.mode" {
			if v.Value != string(model.DialogueGuided) || v.Source != model.ConfigSourceDefault {
				t.Errorf("an unset mode should show as the guided default, got %q from %v", v.Value, v.Source)
			}
			return
		}
	}
	t.Error("the effective config should list dialogue.mode")
}
