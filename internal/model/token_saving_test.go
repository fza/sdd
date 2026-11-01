package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestResolveTokenSaving(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		want    bool
		invalid bool
	}{
		{name: "unset", value: "", want: true},
		{name: "on", value: "on", want: true},
		{name: "off", value: "off", want: false},
		{name: "unknown value", value: "false", invalid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := model.ResolveTokenSaving(model.SessionsConfig{TokenSaving: tc.value})
			if tc.invalid {
				if err == nil {
					t.Error("a value other than on or off should be a config error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Error("only off should turn token saving off")
			}
		})
	}
}

func TestTokenSavingIsReportedOnPerRepoLayers(t *testing.T) {
	set := model.BaseConfig{Sessions: model.SessionsConfig{TokenSaving: "off"}}
	ignored := model.PerRepoIgnoredSettings(set)
	if len(ignored) != 1 || ignored[0].Key != "sessions.token_saving" || ignored[0].Reason == "" {
		t.Errorf("a per-repo layer setting token saving should be reported with its reason, got %+v", ignored)
	}
	if got := model.PerRepoIgnoredSettings(model.BaseConfig{}); len(got) != 0 {
		t.Errorf("a per-repo layer leaving it unset should report nothing, got %+v", got)
	}
	if got := model.IgnoredSettings(model.BaseConfig{Sessions: model.SessionsConfig{TokenSaving: "maybe"}}, false); len(got) != 1 {
		t.Errorf("an unknown value should be reported on any layer, got %+v", got)
	}
}
