package model

// committedStrips and localStrips hold each feature's part of HonoredCommitted
// and HonoredLocal. A feature adds its strip in an init of its own file; a
// strip clears the settings its layer may not make and reports whether it
// cleared any.
var committedStrips, localStrips []func(*PerRepoConfig) bool

// HonoredCommitted returns the committed layer without the settings only a
// person's own config, or only the user-global config, may make.
func HonoredCommitted(committed *PerRepoConfig) *PerRepoConfig {
	return honored(committed, committedStrips)
}

// HonoredLocal returns the machine-local layer without the settings only the
// committed config, or only the user-global config, may make.
func HonoredLocal(local *PerRepoConfig) *PerRepoConfig {
	return honored(local, localStrips)
}

// honored returns layer itself when no strip cleared anything, else a copy.
func honored(layer *PerRepoConfig, strips []func(*PerRepoConfig) bool) *PerRepoConfig {
	if layer == nil {
		return nil
	}
	out := *layer
	stripped := false
	for _, strip := range strips {
		if strip(&out) {
			stripped = true
		}
	}
	if !stripped {
		return layer
	}
	return &out
}
