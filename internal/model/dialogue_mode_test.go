package model_test

import (
	"errors"
	"strings"
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
		{name: "auto", yaml: "dialogue:\n  mode: auto\n", want: model.DialogueAuto},
		{name: "assisted", yaml: "dialogue:\n  mode: assisted\n", want: model.DialogueAssisted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := model.ParseConfig([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("a known mode should load: %v", err)
			}
			if got := model.ResolveDialogueMode(cfg); got != tc.want {
				t.Errorf("the configured mode should resolve as set, got %q", got)
			}
		})
	}
}

func TestDialogueModeValidate(t *testing.T) {
	cases := []struct {
		name    string
		mode    model.DialogueMode
		refused bool
	}{
		{name: "unset"},
		{name: "guided", mode: model.DialogueGuided},
		{name: "auto", mode: model.DialogueAuto},
		{name: "assisted", mode: model.DialogueAssisted},
		{name: "former autonomous", mode: "autonomous", refused: true},
		{name: "unknown value", mode: "free", refused: true},
		{name: "wrong case", mode: "Auto", refused: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.mode.Validate()
			if !tc.refused {
				if err != nil {
					t.Errorf("a known or unset mode should pass: %v", err)
				}
				return
			}
			if !errors.Is(err, model.ErrUnknownDialogueMode) {
				t.Fatalf("an unknown mode should be refused, got %v", err)
			}
			for _, part := range []string{string(tc.mode), "guided, auto or assisted"} {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("the refusal should name the value and the valid values, missing %q: %v", part, err)
				}
			}
		})
	}
}

// The agent takes the choices in auto and assisted alike; only guided puts
// them to the person.
func TestDialogueModeAgentAnswers(t *testing.T) {
	for mode, want := range map[model.DialogueMode]bool{"": false, model.DialogueGuided: false, model.DialogueAuto: true, model.DialogueAssisted: true} {
		if got := mode.AgentAnswers(); got != want {
			t.Errorf("%q: AgentAnswers should be %v, got %v", mode, want, got)
		}
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
		{name: "auto in the committed layer", mode: model.DialogueAuto, committed: true, ignored: true},
		{name: "guided in the committed layer", mode: model.DialogueGuided, committed: true, ignored: true},
		{name: "auto in a personal layer", mode: model.DialogueAuto},
		{name: "guided in a personal layer", mode: model.DialogueGuided},
		{name: "unknown value in the committed layer", mode: "autonomous", committed: true, ignored: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := model.IgnoredSettings(model.BaseConfig{Dialogue: model.DialogueConfig{Mode: tc.mode}}, layerOf(tc.committed))
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
	committed := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Dialogue: model.DialogueConfig{Mode: model.DialogueAuto}}, GraphDir: "graph"}
	honored := model.HonoredCommitted(committed)
	if honored.Dialogue.Mode != "" || honored.GraphDir != "graph" {
		t.Errorf("only the dialogue mode should be dropped from the committed layer, got %+v", honored)
	}
	if committed.Dialogue.Mode != model.DialogueAuto {
		t.Error("the layer read should be left as it was")
	}
}

func TestDialogueModeLayering(t *testing.T) {
	auto := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Dialogue: model.DialogueConfig{Mode: model.DialogueAuto}}}
	guided := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Dialogue: model.DialogueConfig{Mode: model.DialogueGuided}}}
	cases := []struct {
		name    string
		base    *model.PerRepoConfig
		overlay *model.PerRepoConfig
		want    model.DialogueMode
	}{
		{name: "higher layer overrides", base: auto, overlay: guided, want: model.DialogueGuided},
		{name: "higher layer turns it on", base: guided, overlay: auto, want: model.DialogueAuto},
		{name: "unset higher layer keeps lower", base: auto, overlay: &model.PerRepoConfig{}, want: model.DialogueAuto},
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
