package model

import (
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// InitConfig is the `init` block: the agent surfaces `sdd init` leaves to a
// project that manages them itself (20261109-153425-d-tac-5dh). Only the
// committed .sdd/config.yaml sets it.
type InitConfig struct {
	Skip []InitSurface `yaml:"skip,omitempty"`
}

// InitSurface is one agent surface init writes.
type InitSurface string

const (
	// InitSurfaceMCP is the sdd server registration of every agent target.
	InitSurfaceMCP InitSurface = "mcp"
	// InitSurfacePermissions is the mcp__sdd__* allow rule.
	InitSurfacePermissions InitSurface = "permissions"
	// InitSurfaceSkills is the skill renders at the configured scope.
	InitSurfaceSkills InitSurface = "skills"
)

// InitSurfaces are the values init.skip takes.
var InitSurfaces = []InitSurface{InitSurfaceMCP, InitSurfacePermissions, InitSurfaceSkills}

// Skips reports whether init leaves surface alone. A value outside
// InitSurfaces skips nothing.
func (c InitConfig) Skips(surface InitSurface) bool {
	return slices.Contains(c.Skip, surface)
}

const committedOnlyReason = "only the committed .sdd/config.yaml may set it"

// IgnoredInitSettings returns what sdd reads past in one layer's init block:
// the whole block outside the committed config, an unknown value inside it.
func IgnoredInitSettings(init InitConfig, source ConfigValueSource) []IgnoredSetting {
	if len(init.Skip) == 0 {
		return nil
	}
	if source != ConfigSourceProject {
		return []IgnoredSetting{{Key: "init.skip", Reason: committedOnlyReason}}
	}
	var ignored []IgnoredSetting
	for _, value := range init.Skip {
		if !slices.Contains(InitSurfaces, value) {
			ignored = append(ignored, IgnoredSetting{Key: "init.skip", Reason: unknownInitSurface(value)})
		}
	}
	return ignored
}

// InitSettingRefusal is why `sdd config set` refuses key under the init block
// with value: an unknown init.skip value, else the layer, since neither the
// global nor the machine-local config may set it. Nil for any other key.
func InitSettingRefusal(key, value, layer string) error {
	if key != "init" && !strings.HasPrefix(key, "init.") {
		return nil
	}
	if key == "init.skip" {
		var values []InitSurface
		err := yaml.Unmarshal([]byte(value), &values)
		if err != nil {
			values = []InitSurface{InitSurface(value)}
		}
		for _, v := range values {
			if !slices.Contains(InitSurfaces, v) {
				return fmt.Errorf("init.skip = %s: %s", value, unknownInitSurface(v))
			}
		}
	}
	return fmt.Errorf("%s is set only in the committed .sdd/config.yaml, never in the %s config", key, layer)
}

// initSkipBlock carries a pointing repository's init.skip into its rewritten
// config: the agent surfaces stay in that repository.
func initSkipBlock(cfg PerRepoConfig) string {
	if len(cfg.Init.Skip) == 0 {
		return ""
	}
	values := make([]string, len(cfg.Init.Skip))
	for i, value := range cfg.Init.Skip {
		values[i] = string(value)
	}
	return "\n# Agent surfaces `sdd init` leaves to this repository: mcp, permissions,\n" +
		"# skills.\n" +
		"init:\n" +
		"  skip: [" + strings.Join(values, ", ") + "]\n"
}

func unknownInitSurface(value InitSurface) string {
	return fmt.Sprintf("%q is not mcp, permissions or skills", value)
}
