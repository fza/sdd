package proctest_test

import (
	"strings"
	"testing"
)

// A person's next session names the captures an autonomous session parked,
// so they learn of them.
func TestUnattended_AGuidedOpeningNamesParkedCaptures(t *testing.T) {
	world, session, instance := blockedAutonomousCapture(t, "unattended-framing")
	parkAfterSecondBlock(t, session, instance)

	world.SetDialogue("guided")
	opened := world.Open(t, "person")
	blocks, err := opened.WF.Framing(t.Context(), world.Identity)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(blocks, "\n\n")
	for _, want := range []string{`"A tactical gap: the fixture observes something."`, "capture parked at reviseOrOverride since", "blocked again", "Resume: resume_session " + string(session.ID)} {
		if !strings.Contains(text, want) {
			t.Errorf("a guided opening should name the parked capture, missing %q in %q", want, text)
		}
	}
}
