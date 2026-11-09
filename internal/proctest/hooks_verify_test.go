// Configured hooks at the capture's deep verification step, the verify
// stage (20261109-005138-d-tac-i61, 20261109-004729-d-cpt-xs5).
package proctest_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

func newHookedDeepWorld(t *testing.T, connID string, hooks ...sdd.Hook) (*proctest.World, *proctest.Session, *proctest.ScriptedHooks) {
	t.Helper()
	scripted := &proctest.ScriptedHooks{Configured: hooks, Scripts: map[string]proctest.HookScript{}}
	world, session := newDeepWorld(t, connID, sdd.VerifierPolicy{}, proctest.WithHooks(scripted))
	return world, session, scripted
}

func TestHooks_VerifyEventsFireAroundTheDeepExchange(t *testing.T) {
	world, session, scripted := newHookedDeepWorld(t, "hooks-verify",
		sdd.Hook{HookConfig: model.HookConfig{Event: "capture.verify.start", Command: "status"}},
		sdd.Hook{HookConfig: model.HookConfig{Event: "capture.verify.round", Command: "status"}},
		sdd.Hook{HookConfig: model.HookConfig{Event: "capture.verify", Command: "status"}},
	)
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{
		map[string]any{"verdict": "the gap names no subject", "axis": "inconsistent", "severity": "medium", "edits": []any{deepEdit("observes something.", "observes the attachment writer dropping a file.")}},
	})
	serve := x.pickUp(t)
	proctest.RequireStep(t, serve, "deepAnswer")

	for _, event := range []string{"capture.verify.start", "capture.verify.round"} {
		if runs := scripted.RunsAt(event); len(runs) != 1 {
			t.Fatalf("%s should fire once by the first delivery, fired %d times", event, len(runs))
		}
	}
	if runs := scripted.RunsAt("capture.verify"); len(runs) != 0 {
		t.Fatalf("moving between the deep exchange's steps should not leave the verify step, left %d times", len(runs))
	}
	round := scripted.RunsAt("capture.verify.round")[0].Payload
	verify, _ := round["verify"].(map[string]any)
	if round["step"] != "verify" || round["phase"] != "round" || verify["mode"] != "deep" || verify["launch"] != "subagent" || verify["round"] != float64(1) || verify["open"] != float64(1) {
		t.Fatalf("a round should name the verify step and carry the verifier's mode, launch, round and open findings, got %v", round)
	}

	serve = session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	proctest.RequireStep(t, serve, "deepProposal")
	session.Answer(t, x.instance, "deepProposal", "accept", map[string]any{"body": "A tactical gap: the fixture observes the attachment writer dropping a file."}, "")
	left := scripted.RunsAt("capture.verify")
	if len(left) != 1 || left[0].Payload["transition"] != "assemble" {
		t.Fatalf("accepting a changed draft should leave the verify step once, back to assemble, got %+v", left)
	}
	if _, ok := left[0].Payload["findings"]; ok {
		t.Errorf("leaving the verify step from a step without its channel should carry no findings, got %v", left[0].Payload["findings"])
	}
}

func TestHooks_VerifyHighFindingHoldsTheWrite(t *testing.T) {
	world, session, scripted := newHookedDeepWorld(t, "hooks-verify-hold",
		sdd.Hook{HookConfig: model.HookConfig{Event: "capture.verify", Command: "lint", Findings: true}},
		sdd.Hook{HookConfig: model.HookConfig{Event: "entry.written", Command: "notify"}},
	)
	scripted.Scripts["lint"] = proctest.HookScript{Output: highFindingOutput}
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{})
	serve := x.pickUp(t)
	proctest.RequireStep(t, serve, "deepDone")
	if !hasDiagnostic(serve, "the opening sentence is passive") {
		t.Fatalf("the held verify step should name the hook's finding, diagnostics %v", serve.Diagnostics)
	}
	left := scripted.RunsAt("capture.verify")
	if len(left) != 1 || left[0].Payload["transition"] != "checked" || left[0].Payload["findings"] == nil {
		t.Fatalf("leaving the verify step through its channel should name the transition and carry the findings so far, got %+v", left)
	}
	if runs := scripted.RunsAt("entry.written"); len(runs) != 0 {
		t.Fatalf("a held verify step should publish nothing, entry.written ran %d times", len(runs))
	}

	scripted.Scripts["lint"] = proctest.HookScript{}
	serve = session.Report(t, x.instance, nil)
	proctest.RequireStep(t, serve, "verifySummary")
	if runs := scripted.RunsAt("entry.written"); len(runs) != 1 {
		t.Fatalf("once the hooks pass, the capture should publish once, entry.written ran %d times", len(runs))
	}
}
