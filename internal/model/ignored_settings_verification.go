package model

// dialogue.mode is a person's own; verify values outside their closed sets
// are read past in any layer; recheck is set only in the committed config.
func init() {
	ignoredSettingRules = append(ignoredSettingRules, func(layer BaseConfig, source ConfigValueSource) []IgnoredSetting {
		var ignored []IgnoredSetting
		if reason := ignoredDialogueMode(layer.Dialogue.Mode, source == ConfigSourceProject); reason != "" {
			ignored = append(ignored, IgnoredSetting{Key: "dialogue.mode", Reason: reason})
		}
		return append(ignored, ignoredVerifyValues(layer.Verify)...)
	})
	committedStrips = append(committedStrips, func(cfg *PerRepoConfig) bool {
		if cfg.Dialogue.Mode == "" {
			return false
		}
		cfg.Dialogue.Mode = ""
		return true
	})
	localStrips = append(localStrips, func(cfg *PerRepoConfig) bool {
		if len(cfg.Recheck.Responsible) == 0 {
			return false
		}
		cfg.Recheck = RecheckConfig{}
		return true
	})
}
