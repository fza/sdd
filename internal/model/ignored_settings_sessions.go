package model

func init() {
	ignoredSettingRules = append(ignoredSettingRules, func(layer BaseConfig, source ConfigValueSource) []IgnoredSetting {
		// The daily sweep collects every project's sessions under one value.
		if source != ConfigSourceGlobal && layer.Sessions.Retention != "" {
			return []IgnoredSetting{{Key: "sessions.retention", Reason: globalOnly.reason()}}
		}
		return nil
	})
}
