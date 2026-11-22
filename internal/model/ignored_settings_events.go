package model

// Event sharing is the team's choice: only the committed config sets events.
func init() {
	localStrips = append(localStrips, func(cfg *PerRepoConfig) bool {
		if len(cfg.Events.Channels) == 0 {
			return false
		}
		cfg.Events = EventsConfig{}
		return true
	})
}
