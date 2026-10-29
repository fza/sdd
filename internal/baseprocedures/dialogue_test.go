package baseprocedures_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/baseprocedures"
	"github.com/networkteam/sdd/internal/engine"
)

// Every user choice the base procedures hand to an autonomous agent, and
// the one they keep for the user (20261029-150854-d-tac-b2f).
var (
	wantDelegable = []string{
		"bootstrap/propose", "bootstrap/refresh", "capture/playback", "catch-up/junction",
		"engage/moves", "evaluate/junction", "groom/walk", "implementation/closeout",
		"implementation/setup", "interview/junction", "user-dialogue/junction",
	}
	wantReserved = []string{"capture/reviseOrOverride"}
)

func TestBaseProceduresHandOverEveryChoiceButTheOverride(t *testing.T) {
	entries, err := baseprocedures.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var delegable, reserved []string
	for _, entry := range entries {
		spec, err := engine.ParseSpec(entry)
		if err != nil {
			t.Fatalf("%s: %v", entry.ID, err)
		}
		for _, step := range spec.Steps {
			if step.Chooser != engine.ChooserUser {
				continue
			}
			name := spec.Canonical + "/" + step.ID
			if !step.Delegable {
				reserved = append(reserved, name)
				continue
			}
			delegable = append(delegable, name)
			unitName := step.ID
			if step.Render != "" {
				unitName = step.Render
			}
			unit, ok := spec.Units[unitName]
			if !ok || !unitBranchesOnMode(unit) {
				t.Errorf("%s hands its choice to the agent, so its text should branch on dialogueMode", name)
			}
		}
	}
	slices.Sort(delegable)
	slices.Sort(reserved)
	if !slices.Equal(delegable, wantDelegable) {
		t.Errorf("every base user choice but the override should be delegable, got %v", delegable)
	}
	if !slices.Equal(reserved, wantReserved) {
		t.Errorf("only the pre-flight override should stay the user's, got %v", reserved)
	}
}

func unitBranchesOnMode(unit engine.Unit) bool {
	for _, lane := range unit.Lanes {
		if strings.Contains(lane.Text, ".dialogueMode") {
			return true
		}
	}
	return false
}
