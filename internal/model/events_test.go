package model_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestEventsConfig_GitChannels(t *testing.T) {
	cases := []struct {
		name    string
		config  model.EventsConfig
		want    []string
		wantErr bool
	}{
		{name: "none configured", config: model.EventsConfig{}},
		{name: "git and local", config: model.EventsConfig{Channels: map[string]model.EventChannelConfig{
			"wip": {Share: model.EventShareLocal}, "recheck": {Share: model.EventShareGit}, "outside": {Share: model.EventShareGit}, "other": {},
		}}, want: []string{"outside", "recheck"}},
		{name: "unknown share", config: model.EventsConfig{Channels: map[string]model.EventChannelConfig{"recheck": {Share: "hosted"}}}, wantErr: true},
		{name: "invalid channel name", config: model.EventsConfig{Channels: map[string]model.EventChannelConfig{"Re/check": {Share: model.EventShareGit}}}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.config.GitChannels()
			if (err != nil) != tc.wantErr {
				t.Fatalf("GitChannels error = %v, want error %v", err, tc.wantErr)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("GitChannels = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMergeConfig_OverlaysEventChannels(t *testing.T) {
	base := &model.PerRepoConfig{Events: model.EventsConfig{Channels: map[string]model.EventChannelConfig{
		"recheck": {Share: model.EventShareLocal}, "outside": {Share: model.EventShareGit},
	}}}
	overlay := &model.PerRepoConfig{Events: model.EventsConfig{Channels: map[string]model.EventChannelConfig{
		"recheck": {Share: model.EventShareGit}, "outside": {},
	}}}
	merged := model.MergeConfig(base, overlay)
	channels, err := merged.Events.GitChannels()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(channels, []string{"outside", "recheck"}) {
		t.Fatalf("git channels = %v; the overlay's share should win and an empty one inherit", channels)
	}
	if base.Events.Channels["recheck"].Share != model.EventShareLocal {
		t.Fatal("merging should leave the base layer unchanged")
	}
}

func TestValidateEventType(t *testing.T) {
	for _, valid := range []string{"sdd.recheck", "tracker.issue.commented", "a1.b-2"} {
		err := model.ValidateEventType(valid)
		if err != nil {
			t.Errorf("ValidateEventType(%q) = %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "recheck", "Sdd.recheck", "sdd.", ".recheck", "sdd..recheck", "sdd.re check", "sdd.1st"} {
		err := model.ValidateEventType(invalid)
		if err == nil {
			t.Errorf("ValidateEventType(%q) accepted an invalid type", invalid)
		}
	}
}

func TestValidateEventChannel(t *testing.T) {
	for _, valid := range []string{"recheck", "wip", "tracker-2"} {
		err := model.ValidateEventChannel(valid)
		if err != nil {
			t.Errorf("ValidateEventChannel(%q) = %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "Recheck", "re.check", "../wip", "-wip"} {
		err := model.ValidateEventChannel(invalid)
		if err == nil {
			t.Errorf("ValidateEventChannel(%q) accepted an invalid channel", invalid)
		}
	}
}

func TestEventTypeReserved(t *testing.T) {
	if !model.EventTypeReserved("sdd.recheck") {
		t.Error("a type under sdd. should be reserved")
	}
	for _, other := range []string{"sddx.recheck", "tracker.sdd"} {
		if model.EventTypeReserved(other) {
			t.Errorf("%q should not be reserved", other)
		}
	}
}
