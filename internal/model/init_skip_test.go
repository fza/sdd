package model_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestInitConfigSkipsOnlyKnownListedSurfaces(t *testing.T) {
	cfg := model.InitConfig{Skip: []model.InitSurface{model.InitSurfaceMCP, "bogus"}}
	if !cfg.Skips(model.InitSurfaceMCP) {
		t.Error("a listed surface should be skipped")
	}
	for _, surface := range []model.InitSurface{model.InitSurfacePermissions, model.InitSurfaceSkills} {
		if cfg.Skips(surface) {
			t.Errorf("an unlisted surface should not be skipped: %s", surface)
		}
	}
}

func TestIgnoredInitSettings(t *testing.T) {
	cases := []struct {
		name    string
		skip    []model.InitSurface
		source  model.ConfigValueSource
		reasons []string
	}{
		{name: "known values in the committed config", skip: model.InitSurfaces, source: model.ConfigSourceProject},
		{name: "an unknown value in the committed config", skip: []model.InitSurface{model.InitSurfaceMCP, "bogus"}, source: model.ConfigSourceProject, reasons: []string{`"bogus" is not mcp, permissions or skills`}},
		{name: "the local config", skip: []model.InitSurface{model.InitSurfaceMCP}, source: model.ConfigSourceLocal, reasons: []string{"only the committed .sdd/config.yaml may set it"}},
		{name: "nothing set in the local config", source: model.ConfigSourceLocal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var reasons []string
			for _, ignored := range model.IgnoredInitSettings(model.InitConfig{Skip: c.skip}, c.source) {
				if ignored.Key != "init.skip" {
					t.Errorf("the ignored setting should name init.skip, names %s", ignored.Key)
				}
				reasons = append(reasons, ignored.Reason)
			}
			if !slices.Equal(reasons, c.reasons) {
				t.Errorf("the reported reasons should match what is read past, got %q", reasons)
			}
		})
	}
}

func TestInitSettingRefusal(t *testing.T) {
	cases := []struct {
		name   string
		key    string
		value  string
		refuse string
	}{
		{name: "another key", key: "init_skip", value: "mcp"},
		{name: "an unknown value", key: "init.skip", value: "bogus", refuse: `"bogus" is not mcp, permissions or skills`},
		{name: "an unknown value in a list", key: "init.skip", value: "[skills, bogus]", refuse: `"bogus" is not mcp, permissions or skills`},
		{name: "a known value", key: "init.skip", value: "[mcp, skills]", refuse: "set only in the committed .sdd/config.yaml, never in the local config"},
		{name: "the block", key: "init", value: "x", refuse: "set only in the committed .sdd/config.yaml"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := model.InitSettingRefusal(c.key, c.value, "local")
			switch {
			case c.refuse == "" && err != nil:
				t.Errorf("a key outside the init block should pass, got %v", err)
			case c.refuse != "" && (err == nil || !strings.Contains(err.Error(), c.refuse)):
				t.Errorf("the set should be refused for its own reason, got %v", err)
			}
		})
	}
}

func TestInitIsHonoredOnlyFromTheCommittedLayer(t *testing.T) {
	committed := &model.PerRepoConfig{Init: model.InitConfig{Skip: []model.InitSurface{model.InitSurfaceSkills}}}
	local := &model.PerRepoConfig{Init: model.InitConfig{Skip: []model.InitSurface{model.InitSurfaceMCP}}}

	merged := model.MergeConfig(committed, model.HonoredLocal(local))

	if !slices.Equal(merged.Init.Skip, committed.Init.Skip) {
		t.Errorf("the local layer should not change init.skip, got %v", merged.Init.Skip)
	}
	if fromGlobal := model.MergeConfig(&model.PerRepoConfig{}, committed); !slices.Equal(fromGlobal.Init.Skip, committed.Init.Skip) {
		t.Errorf("the committed layer should carry init.skip over the global one, got %v", fromGlobal.Init.Skip)
	}
}

func TestFormatPointerConfigsCarryInitSkip(t *testing.T) {
	cfg := model.PerRepoConfig{ProjectDir: "../work-sdd", GraphRepo: "example.test/acme/graph", SkillScope: model.ScopeProject, Init: model.InitConfig{Skip: []model.InitSurface{model.InitSurfaceMCP, model.InitSurfaceSkills}}}
	for name, written := range map[string]string{
		"project_dir": model.FormatPointerConfig(cfg),
		"graph_repo":  model.FormatGraphRepoPointerConfig(cfg),
	} {
		t.Run(name, func(t *testing.T) {
			parsed, err := model.ParseConfig([]byte(written))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(parsed.Init.Skip, cfg.Init.Skip) {
				t.Errorf("the pointer config should keep init.skip:\n%s", written)
			}
		})
	}
	without := model.FormatPointerConfig(model.PerRepoConfig{ProjectDir: "../work-sdd"})
	if strings.Contains(without, "init:") {
		t.Errorf("a pointer config without init.skip should write no init block:\n%s", without)
	}
}
