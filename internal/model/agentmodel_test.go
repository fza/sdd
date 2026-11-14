package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// A harness names a model by its full ID (claude-opus-5-5) or by an alias
// (opus), as a dispatch does.
func TestHoldNextModel_MatchesAnOpusOrFableModel(t *testing.T) {
	for agentModel, want := range map[string]bool{
		"claude-opus-5-5":   true,
		"opus":              true,
		"Claude Opus 5.5":   true,
		"claude-fable-5":    true,
		"FABLE":             true,
		"claude-sonnet-4-5": false,
		"sonnet":            false,
		"haiku":             false,
		"gpt-5.5":           false,
		"":                  false,
	} {
		t.Run(agentModel, func(t *testing.T) {
			if got := model.HoldNextModel(agentModel); got != want {
				t.Fatalf("model %q should get the hold-next line only when it names Opus or Fable, got %v", agentModel, got)
			}
		})
	}
}
