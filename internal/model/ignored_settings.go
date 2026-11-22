package model

// IgnoredSetting is a setting a config layer carries that sdd reads past:
// a value it does not know, or a key that layer may not set.
type IgnoredSetting struct {
	Key    string
	Reason string
}

// ignoredSettingRules hold each feature's part of IgnoredSettings. A feature
// adds its rule in an init of its own file, so no two features edit one list.
var ignoredSettingRules []func(layer BaseConfig, source ConfigValueSource) []IgnoredSetting

// IgnoredSettings returns the settings in one config layer that sdd reads
// past. source names the layer: global, project (the shared
// .sdd/config.yaml) or local.
func IgnoredSettings(layer BaseConfig, source ConfigValueSource) []IgnoredSetting {
	var ignored []IgnoredSetting
	for _, rule := range ignoredSettingRules {
		ignored = append(ignored, rule(layer, source)...)
	}
	return ignored
}

// settingScope names the config layers that may make a setting.
type settingScope int

const (
	globalOnly settingScope = iota + 1
	personalOnly
)

// reason says why a layer outside the scope may not make the setting.
func (s settingScope) reason() string {
	switch s {
	case globalOnly:
		return "only the user-global config may set it"
	case personalOnly:
		return "only the user-global config or config.local.yaml may set it"
	}
	return ""
}
