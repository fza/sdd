package model

func init() {
	ignoredSettingRules = append(ignoredSettingRules, func(layer BaseConfig, source ConfigValueSource) []IgnoredSetting {
		// The daily pass and every scan on the machine share one rule file.
		if source != ConfigSourceGlobal && layer.Secrets.RulesUpdate != "" {
			return []IgnoredSetting{{Key: secretRulesUpdateKey, Reason: globalOnly.reason()}}
		}
		return nil
	})
}
