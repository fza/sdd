package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// acceptedDeepEdit drives a guided capture through a deep exchange whose
// proposal the drafter accepts, back to the playback of the changed draft.
func acceptedDeepEdit(t *testing.T, connID string) (*proctest.World, *proctest.Session, string) {
	t.Helper()
	world, session := newDeepWorld(t, connID, sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{map[string]any{"verdict": "the gap names no subject", "axis": "inconsistent", "severity": "medium", "edits": []any{deepEdit("observes something.", "observes the attachment writer dropping a file.")}}})
	x.pickUp(t)
	session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	serve := session.Answer(t, x.instance, "deepProposal", "accept", nil, "")
	proctest.RequireStep(t, serve, "playback")
	world.LLM.GuideFindings = []proctest.GuideFinding{{
		Reasoning: "'the attachment writer' names a component the entry never introduces",
		Axis:      "stranding", Quote: "the attachment writer", Repair: "write-in", Severity: "substantive",
	}}
	return world, session, x.instance
}

// The deep exchange settled the passages its accepted proposal wrote, so the
// writing guide does not judge them again after the user confirms them.
func TestCaptureDeep_TheGuideLeavesAnAcceptedProposalAlone(t *testing.T) {
	world, session, instance := acceptedDeepEdit(t, "deep-guide-settled")
	serve := session.Answer(t, instance, "playback", "confirm", nil, "yes, with the verifier's change")
	proctest.RequireStep(t, serve, "verifySummary")
	if got := world.LLM.Calls("writing-guide"); got != 1 {
		t.Fatalf("the guide should judge the approved draft once and leave the verifier's edit alone, ran %d times", got)
	}
}

// The user's confirm at an open point settles the exchange as the draft
// stands, so it runs no delta check.
func TestCaptureDeep_AConfirmAtAnOpenPointGetsNoDeltaCheck(t *testing.T) {
	world, session := newDeepWorld(t, "deep-guide-owner", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{map[string]any{
		"verdict": "whether this gap is in scope is the owner's call", "axis": "aspiration", "severity": "medium", "owner": true,
		"pointers": []any{map[string]any{"field": "body", "quote": "observes something"}},
	}})
	x.pickUp(t)
	session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	requireOpenPoint(t, session.Answer(t, x.instance, "deepProposal", "accept", nil, ""), ownerLine)
	proctest.RequireStep(t, session.Answer(t, x.instance, "playback", "confirm", nil, "it is in scope"), "verifySummary")
	if got := world.LLM.Calls("writing-guide"); got != 1 {
		t.Fatalf("the user's settling confirm should get no delta check, the guide ran %d times", got)
	}
}

// A later adjust of the user's gets its delta check, of the lines the user
// changed only, not the ones the accepted proposal wrote.
func TestCaptureDeep_TheDeltaCheckAfterAnAcceptedProposalJudgesTheUsersLines(t *testing.T) {
	world, session, instance := acceptedDeepEdit(t, "deep-guide-delta")
	serve := session.Answer(t, instance, "playback", "adjust",
		map[string]any{"body": "A tactical gap: the fixture observes the attachment writer dropping a file.\n\nIt shows on every run."}, "add the frequency")
	proctest.RequireStep(t, serve, "playback")
	session.Answer(t, instance, "playback", "confirm", nil, "now it is right")
	prompts := world.LLM.Prompts("writing-guide")
	if len(prompts) != 2 {
		t.Fatalf("the user's adjust should get one delta check, got %d guide calls", len(prompts))
	}
	delta := prompts[1].UserPrompt
	_, changed, found := strings.Cut(delta, "Changed since the last check")
	if !found || !strings.Contains(changed, "It shows on every run.") || strings.Contains(changed, "attachment writer") {
		t.Fatalf("the delta check should judge the user's line and leave out the verifier's:\n%s", delta)
	}
}
