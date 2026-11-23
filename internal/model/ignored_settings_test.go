package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestIgnoredSettingsReportsSessionRetentionOutsideTheGlobalConfig(t *testing.T) {
	layer := model.BaseConfig{Sessions: model.SessionsConfig{Retention: "30d"}}
	for _, source := range []model.ConfigValueSource{model.ConfigSourceProject, model.ConfigSourceLocal} {
		ignored := model.IgnoredSettings(layer, source)
		if len(ignored) != 1 || ignored[0].Key != "sessions.retention" || ignored[0].Reason == "" {
			t.Errorf("%s layer: the machine-wide sweep reads only the global value, so this one should be reported, got %v", source, ignored)
		}
	}
	if got := model.IgnoredSettings(layer, model.ConfigSourceGlobal); len(got) != 0 {
		t.Errorf("the global config may set the session retention, got %v", got)
	}
	if got := model.IgnoredSettings(model.BaseConfig{}, model.ConfigSourceProject); len(got) != 0 {
		t.Errorf("a layer setting nothing has nothing to read past, got %v", got)
	}
}
