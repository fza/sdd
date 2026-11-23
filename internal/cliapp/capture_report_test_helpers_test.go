package cliapp_test

func captureReport() map[string]any {
	return map[string]any{
		"body":        "The platform graph lacks a record of how the home project consumes its API, so platform changes break the home project unnoticed.",
		"entryKind":   "gap",
		"layer":       "tactical",
		"topics":      []string{"testing/connected"},
		"confidence":  "low",
		"widenReport": "searched the platform graph for consumer records; it holds none.",
	}
}
