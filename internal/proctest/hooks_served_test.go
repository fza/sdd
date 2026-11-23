// A hook's non-blocking findings are served to the agent like the other
// findings of the step they join.
package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
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
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.assemble", Command: "lint", Findings: true}})
	scripted.Scripts["lint"] = proctest.HookScript{Output: mediumFindingOutput}

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	serve = session.Report(t, instance, captureDraft())
	requireServedHookFinding(t, serve, "assembleHookReview")

	serve = session.Answer(t, instance, "assembleHookReview", "proceed", nil, "")
	proctest.RequireStep(t, serve, "playback")
	runs := scripted.RunsAt("capture.assemble")
	if len(runs) != 2 {
		t.Errorf("proceeding should leave assemble again through its hooks, ran %d times", len(runs))
	}

	scripted.Scripts["lint"] = proctest.HookScript{Output: `{"findings": [{"severity": "low", "category": "prose-lint", "observation": "another finding"}]}`}
	serve = session.Answer(t, instance, "playback", "adjust", map[string]any{"body": "A tactical gap: the fixture observes something, adjusted."}, "adjust it")
	proctest.RequireStep(t, serve, "assembleHookReview")
	if !strings.Contains(serve.Instructions, "another finding") {
		t.Errorf("findings the agent has not weighed yet should be served again, served %q", serve.Instructions)
	}
}

// A hook whose findings differ on every run, such as one stamping a time,
// still lets the agent proceed once it weighed them for this draft.
func TestHooks_ProceedingPastVaryingFindingsDoesNotLoop(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.assemble", Command: "lint", Findings: true}})
	finding := func(n string) string {
		return `{"findings": [{"severity": "medium", "category": "prose-lint", "observation": "run ` + n + `"}]}`
	}
	scripted.Scripts["lint"] = proctest.HookScript{Varying: []string{finding("1"), finding("2"), finding("3")}}

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "assembleHookReview")
	serve = session.Answer(t, instance, "assembleHookReview", "proceed", nil, "")
	proctest.RequireStep(t, serve, "playback")
}

func TestHooks_GuideMediumFindingIsServedWithTheGuidesFindings(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.guide", Command: "lint", Findings: true}})
	scripted.Scripts["lint"] = proctest.HookScript{Output: mediumFindingOutput}

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Report(t, instance, captureDraft())
	serve = session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	requireServedHookFinding(t, serve, "guideReview")

	serve = session.Answer(t, instance, "guideReview", "proceed", nil, "")
	proctest.RequireStep(t, serve, "verifySummary")
}

// Weighing the assemble hooks' findings weighs those alone: the guide hooks'
// findings on the revised draft are still served at guide.
func TestHooks_WeighingAssembleFindingsLeavesTheGuideFindingsToBeServed(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t,
		sdd.Hook{HookConfig: model.HookConfig{Event: "capture.assemble", Command: "assemble-lint", Findings: true}},
		sdd.Hook{HookConfig: model.HookConfig{Event: "capture.guide", Command: "guide-lint", Findings: true}},
	)
	revised := `{"findings": [{"severity": "medium", "category": "prose-lint", "observation": "the revision repeats itself"}]}`
	scripted.Scripts["assemble-lint"] = proctest.HookScript{Varying: []string{"", mediumFindingOutput}}
	scripted.Scripts["guide-lint"] = proctest.HookScript{Varying: []string{mediumFindingOutput, revised}}

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	serve = session.Report(t, instance, captureDraft())
	requireServedHookFinding(t, serve, "guideReview")
	serve = session.Answer(t, instance, "guideReview", "revise", map[string]any{"body": "A tactical gap: the fixture observes something, revised."}, "")
	requireServedHookFinding(t, serve, "assembleHookReview")
	serve = session.Answer(t, instance, "assembleHookReview", "proceed", nil, "")
	proctest.RequireStep(t, serve, "guideReview")
	if !strings.Contains(serve.Instructions, "the revision repeats itself") {
		t.Errorf("the guide hook's finding on the revised draft should be served, served %q", serve.Instructions)
	}
}

// The writing guide judges a capture once, but the hooks judge every
// revision: a medium hook finding on a revised draft is served even though
// the guide's own findings were already reviewed.
func TestHooks_GuideMediumFindingOnARevisedDraftIsServed(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.guide", Command: "lint", Findings: true}})
	revised := `{"findings": [{"severity": "medium", "category": "prose-lint", "observation": "the revision repeats itself"}]}`
	scripted.Scripts["lint"] = proctest.HookScript{Varying: []string{mediumFindingOutput, revised}}

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	serve = session.Report(t, instance, captureDraft())
	requireServedHookFinding(t, serve, "guideReview")
	serve = session.Answer(t, instance, "guideReview", "proceed", nil, "")
	proctest.RequireStep(t, serve, "playback")

	serve = session.Answer(t, instance, "playback", "adjust", map[string]any{"body": "A tactical gap: the fixture observes something, adjusted."}, "adjust it")
	proctest.RequireStep(t, serve, "guideReview")
	if !strings.Contains(serve.Instructions, "the revision repeats itself") {
		t.Errorf("the hook's finding on the revised draft should be served, served %q", serve.Instructions)
	}
	serve = session.Answer(t, instance, "guideReview", "proceed", nil, "")
	proctest.RequireStep(t, serve, "playback")
}
