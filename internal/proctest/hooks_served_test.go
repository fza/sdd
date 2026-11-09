// A hook's non-blocking findings are served to the agent like the other
// findings of the step they join (20261109-005138-d-tac-i61).
package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

const (
	mediumFindingOutput = `{"findings": [{"severity": "medium", "category": "prose-lint", "observation": "the second sentence repeats the first"}]}`
	mediumObservation   = "prose-lint: the second sentence repeats the first"
)

func requireServedHookFinding(t *testing.T, serve *sdd.WorkflowServe, step string) {
	t.Helper()
	proctest.RequireStep(t, serve, step)
	if !strings.Contains(serve.Instructions, mediumObservation) {
		t.Fatalf("the hook's medium finding should be served to the agent, served %q", serve.Instructions)
	}
}

func TestHooks_AssembleMediumFindingIsServed(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{Event: "capture.assemble.left", Command: "lint", Findings: true})
	scripted.Scripts["lint"] = proctest.HookScript{Output: mediumFindingOutput}

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	serve = session.Report(t, instance, captureDraft())
	requireServedHookFinding(t, serve, "assembleHookReview")

	serve = session.Answer(t, instance, "assembleHookReview", "proceed", nil, "")
	proctest.RequireStep(t, serve, "playback")
	if runs := scripted.RunsAt("capture.assemble.left"); len(runs) != 2 {
		t.Errorf("proceeding should leave assemble again through its hooks, ran %d times", len(runs))
	}

	scripted.Scripts["lint"] = proctest.HookScript{Output: `{"findings": [{"severity": "low", "category": "prose-lint", "observation": "another finding"}]}`}
	serve = session.Answer(t, instance, "playback", "adjust", map[string]any{"body": "A tactical gap: the fixture observes something, adjusted."}, "adjust it")
	proctest.RequireStep(t, serve, "assembleHookReview")
	if !strings.Contains(serve.Instructions, "another finding") {
		t.Errorf("findings the agent has not weighed yet should be served again, served %q", serve.Instructions)
	}
}

func TestHooks_GuideMediumFindingIsServedWithTheGuidesFindings(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{Event: "capture.guide.left", Command: "lint", Findings: true})
	scripted.Scripts["lint"] = proctest.HookScript{Output: mediumFindingOutput}

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Report(t, instance, captureDraft())
	serve = session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	requireServedHookFinding(t, serve, "guideReview")

	serve = session.Answer(t, instance, "guideReview", "proceed", nil, "")
	proctest.RequireStep(t, serve, "verifySummary")
}

func TestHooks_PreflightMediumFindingIsServedWithTheVerifierOff(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{Event: "capture.preflight.left", Command: "lint", Findings: true})
	scripted.Scripts["lint"] = proctest.HookScript{Output: mediumFindingOutput}

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Report(t, instance, captureDraft())
	serve = session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	requireServedHookFinding(t, serve, "preflightVerdict")
	if strings.Contains(serve.Instructions, "light draft verifier") {
		t.Errorf("with the verifier off, the serve should not claim the verifier ran: %q", serve.Instructions)
	}

	serve = session.Answer(t, instance, "preflightVerdict", "proceed", nil, "")
	proctest.RequireStep(t, serve, "verifySummary")
}

func TestHooks_VerifyMediumFindingIsServed(t *testing.T) {
	world, session, scripted := newHookedDeepWorld(t, "hooks-verify-medium", sdd.Hook{Event: "capture.verify.left", Command: "lint", Findings: true})
	scripted.Scripts["lint"] = proctest.HookScript{Output: mediumFindingOutput}
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{})
	serve := x.pickUp(t)
	requireServedHookFinding(t, serve, "verifyHookReview")

	serve = session.Answer(t, x.instance, "verifyHookReview", "proceed", nil, "")
	proctest.RequireStep(t, serve, "verifySummary")
}
