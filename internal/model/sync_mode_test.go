package model_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestSyncModeAtConfigLoad(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		want    model.SyncMode
		refused bool
	}{
		{name: "unset", yaml: "sync:\n  cooldown: 5m\n", want: model.SyncModeManual},
		{name: "manual", yaml: "sync:\n  mode: manual\n", want: model.SyncModeManual},
		{name: "auto", yaml: "sync:\n  mode: auto\n", want: model.SyncModeAuto},
		{name: "unknown value", yaml: "sync:\n  mode: always\n", refused: true},
		{name: "wrong case", yaml: "sync:\n  mode: Auto\n", refused: true},
		{name: "not a scalar", yaml: "sync:\n  mode: [auto]\n", refused: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := model.ParseConfig([]byte(tc.yaml))
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), "sync.mode") {
					t.Fatalf("an unknown mode should be refused naming the key, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("a known mode should load: %v", err)
			}
			if got := model.ResolveSyncMode(cfg); got != tc.want {
				t.Errorf("the effective mode should follow the file, got %q", got)
			}
		})
	}
}

func TestSyncModeRefusedInGlobalLayer(t *testing.T) {
	var global model.BaseConfig
	err := model.UnmarshalYAML([]byte("sync:\n  mode: sometimes\n"), &global)
	if err == nil || !strings.Contains(err.Error(), "sync.mode") {
		t.Fatalf("the global layer should refuse an unknown mode naming the key, got %v", err)
	}
}

func TestSyncModeLayering(t *testing.T) {
	auto := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Sync: model.SyncConfig{Mode: model.SyncModeAuto}}}
	manual := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Sync: model.SyncConfig{Mode: model.SyncModeManual}}}
	cases := []struct {
		name    string
		base    *model.PerRepoConfig
		overlay *model.PerRepoConfig
		want    model.SyncMode
	}{
		{name: "higher layer overrides", base: auto, overlay: manual, want: model.SyncModeManual},
		{name: "higher layer turns it on", base: manual, overlay: auto, want: model.SyncModeAuto},
		{name: "unset higher layer keeps lower", base: auto, overlay: &model.PerRepoConfig{}, want: model.SyncModeAuto},
		{name: "unset everywhere", base: &model.PerRepoConfig{}, overlay: &model.PerRepoConfig{}, want: model.SyncModeManual},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.ResolveSyncMode(model.MergeConfig(tc.base, tc.overlay)); got != tc.want {
				t.Errorf("the merged mode should come from the highest layer setting it, got %q", got)
			}
		})
	}
}

func TestSyncModeEffectiveDefault(t *testing.T) {
	for _, v := range model.EffectiveConfigValues(model.BaseConfig{}, nil, nil) {
		if v.Key == "sync.mode" {
			if v.Value != string(model.SyncModeManual) || v.Source != model.ConfigSourceDefault {
				t.Errorf("an unset mode should show as the manual default, got %q from %v", v.Value, v.Source)
			}
			return
		}
	}
	t.Error("the effective config should list sync.mode")
}

func TestDecidePush(t *testing.T) {
	cases := []struct {
		name        string
		mode        model.SyncMode
		hasUpstream bool
		ahead       int
		graphAhead  int
		want        model.PushVerdict
	}{
		{name: "manual never pushes", mode: model.SyncModeManual, hasUpstream: true, ahead: 2, graphAhead: 2, want: model.PushOff},
		{name: "unset mode never pushes", mode: "", hasUpstream: true, ahead: 1, graphAhead: 1, want: model.PushOff},
		{name: "no upstream", mode: model.SyncModeAuto, ahead: 1, graphAhead: 1, want: model.PushNoUpstream},
		{name: "nothing ahead", mode: model.SyncModeAuto, hasUpstream: true, want: model.PushNothing},
		{name: "a foreign commit ahead", mode: model.SyncModeAuto, hasUpstream: true, ahead: 3, graphAhead: 2, want: model.PushUnpublished},
		{name: "only graph commits ahead", mode: model.SyncModeAuto, hasUpstream: true, ahead: 2, graphAhead: 2, want: model.PushReady},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.DecidePush(tc.mode, tc.hasUpstream, tc.ahead, tc.graphAhead); got != tc.want {
				t.Errorf("the push verdict should follow mode, upstream and commits ahead, got %q", got)
			}
		})
	}
}

func TestDecidePull(t *testing.T) {
	cases := []struct {
		name          string
		mode          model.SyncMode
		hasUpstream   bool
		incoming      int
		graphIncoming int
		clean         bool
		want          model.PullVerdict
	}{
		{name: "manual never pulls", mode: model.SyncModeManual, hasUpstream: true, incoming: 1, graphIncoming: 1, clean: true, want: model.PullOff},
		{name: "no upstream", mode: model.SyncModeAuto, incoming: 1, graphIncoming: 1, clean: true, want: model.PullNoUpstream},
		{name: "nothing incoming", mode: model.SyncModeAuto, hasUpstream: true, clean: true, want: model.PullNothing},
		{name: "a foreign commit incoming", mode: model.SyncModeAuto, hasUpstream: true, incoming: 2, graphIncoming: 1, clean: true, want: model.PullForeign},
		{name: "dirty tree", mode: model.SyncModeAuto, hasUpstream: true, incoming: 1, graphIncoming: 1, want: model.PullDirty},
		{name: "only graph commits on a clean tree", mode: model.SyncModeAuto, hasUpstream: true, incoming: 2, graphIncoming: 2, clean: true, want: model.PullReady},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.DecidePull(tc.mode, tc.hasUpstream, tc.incoming, tc.graphIncoming, tc.clean); got != tc.want {
				t.Errorf("the pull verdict should follow mode, upstream, incoming commits and tree state, got %q", got)
			}
		})
	}
}

func TestGraphMergeMessagePassesThePushRule(t *testing.T) {
	if msg := model.GraphMergeMessage("origin/main"); !regexp.MustCompile(`^sdd:`).MatchString(msg) {
		t.Errorf("the merge message should count as a graph commit, got %q", msg)
	}
}
