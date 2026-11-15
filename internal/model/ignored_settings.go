package model

// IgnoredSetting is a setting a config layer carries that sdd reads past: a
// key that layer may not set.
type IgnoredSetting struct {
	Key    string
	Reason string
}

const globalOnlyReason = "only the user-global config may set it"

// IgnoredSettings returns the settings in one config layer that sdd reads
// past. source names the layer: global, project (the shared
// .sdd/config.yaml) or local.
func IgnoredSettings(layer BaseConfig, source ConfigValueSource) []IgnoredSetting {
	var ignored []IgnoredSetting
	// The daily sweep collects every project's sessions under one value.
	if source != ConfigSourceGlobal && layer.Sessions.Retention != "" {
		ignored = append(ignored, IgnoredSetting{Key: "sessions.retention", Reason: globalOnlyReason})
	}
	// The daily pass and every scan on the machine share one rule file.
	if source != ConfigSourceGlobal && layer.Secrets.RulesUpdate != "" {
		ignored = append(ignored, IgnoredSetting{Key: secretRulesUpdateKey, Reason: globalOnlyReason})
	}
	return ignored
}
