// Configured hooks at the steps of the shipped procedures through the real
// application (20261109-005138-d-tac-i61).
package proctest_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

const highFindingOutput = `{"findings": [{"severity": "high", "category": "prose-lint", "observation": "the opening sentence is passive"}]}`

func newHookedCaptureWorld(t *testing.T, hooks ...sdd.Hook) (*proctest.World, *proctest.Session, *proctest.ScriptedHooks) {
	t.Helper()
	scripted := &proctest.ScriptedHooks{Configured: hooks, Scripts: map[string]proctest.HookScript{}}
	world, session := newCaptureWorld(t, "hooks", proctest.WithHooks(scripted))
	return world, session, scripted
}

func openErr(t *testing.T, hooks ...sdd.Hook) error {
	t.Helper()
	scripted := &proctest.ScriptedHooks{Configured: hooks}
	world := proctest.NewWorld(t, proctest.WithHooks(scripted))
	_, _, err := world.App.OpenWorkflow(t.Context(), world.Identity, "proctest", sdd.WorkflowOpenRequest{ClientName: "hooks"})
	return err
}

func TestHooks_PreflightFindingJoinsTheWriteGateAndRoutesToOverride(t *testing.T) {
	world, session, scripted := newHookedCaptureWorld(t,
		sdd.Hook{Event: "capture.preflight.left", Command: "lint", Findings: true},
		sdd.Hook{Event: "entry.written", Command: "notify"},
	)
	scripted.Scripts["lint"] = proctest.HookScript{Output: highFindingOutput}

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Report(t, instance, captureDraft())
	serve = session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	proctest.RequireStep(t, serve, "reviseOrOverride")
	if !strings.Contains(serve.Instructions, "prose-lint: the opening sentence is passive") {
		t.Errorf("the hook's finding should join pre-flight's findings, served %q", serve.Instructions)
	}
	if runs := scripted.RunsAt("entry.written"); len(runs) != 0 {
		t.Fatalf("a blocked write should publish nothing, entry.written ran %d times", len(runs))
	}

	serve = session.Answer(t, instance, "reviseOrOverride", "override", nil, "the lint is wrong here")
	proctest.RequireStep(t, serve, "verifySummary")
	written := scripted.RunsAt("entry.written")
	if len(written) != 1 {
		t.Fatalf("entry.written should fire once after the publish, fired %d times", len(written))
	}
	serve = session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	if entryID := serve.Produced["entryId"]; written[0].Payload["entry"] != entryID {
		t.Errorf("entry.written should name the published entry, got %v", written[0].Payload["entry"])
	}
	proctest.LoadEntry(t, world.GraphDir, serve.Produced["entryId"].(string))
}

func TestHooks_AssembleHighFindingHoldsTheDraft(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{Event: "capture.assemble.left", Command: "lint", Findings: true})
	scripted.Scripts["lint"] = proctest.HookScript{Output: highFindingOutput}

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "assemble")
	if !hasDiagnostic(serve, "the opening sentence is passive") {
		t.Fatalf("the held draft should name the hook's finding, diagnostics %v", serve.Diagnostics)
	}

	scripted.Scripts["lint"] = proctest.HookScript{}
	serve = session.Report(t, instance, map[string]any{"body": "A tactical gap: the fixture observes something, revised."})
	proctest.RequireStep(t, serve, "playback")
}

func TestHooks_GuideHighFindingHoldsTheDraft(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{Event: "capture.guide.left", Command: "lint", Findings: true})
	scripted.Scripts["lint"] = proctest.HookScript{Output: highFindingOutput}

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Report(t, instance, captureDraft())
	serve = session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	proctest.RequireStep(t, serve, "guide")
	if !hasDiagnostic(serve, "the opening sentence is passive") {
		t.Fatalf("the held draft should name the hook's finding, diagnostics %v", serve.Diagnostics)
	}
}

func TestHooks_PayloadCarriesTheStepTheDraftAndItsFindings(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{Event: "capture.preflight.left", Command: "status"})

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Report(t, instance, captureDraft())
	session.Answer(t, instance, "playback", "confirm", nil, "capture it")

	runs := scripted.RunsAt("capture.preflight.left")
	if len(runs) != 1 {
		t.Fatalf("the hook should run once at capture.preflight.left, ran %d times", len(runs))
	}
	payload := runs[0].Payload
	for field, want := range map[string]any{
		"procedure": "capture", "instance": instance, "step": "preflight", "phase": "left",
		"transition": "checked", "project": "proctest", "branch": "main", "session": string(session.ID),
	} {
		if payload[field] != want {
			t.Errorf("payload %s = %v, want %v", field, payload[field], want)
		}
	}
	if _, err := time.Parse(time.RFC3339, payload["time"].(string)); err != nil {
		t.Errorf("payload time should be RFC 3339: %v", err)
	}
	if findings, ok := payload["findings"].([]any); !ok || len(findings) != 0 {
		t.Errorf("a step with a findings channel should carry its findings so far, got %v", payload["findings"])
	}
	draft, _ := payload["draft"].(map[string]any)
	if draft["type"] != "signal" || draft["kind"] != "gap" || draft["layer"] != "tactical" ||
		!strings.Contains(draft["body"].(string), "the fixture observes something") || len(draft["body_hash"].(string)) != 64 {
		t.Errorf("a capture event should carry the draft, got %v", draft)
	}
}

func TestHooks_WildcardMatchesEveryProcedureAndStep(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{Event: "*.*.entered", Command: "status"})
	session.Start(t, "capture", nil)
	for _, event := range []string{"user-dialogue.junction.entered", "capture.assemble.entered"} {
		if len(scripted.RunsAt(event)) != 1 {
			t.Errorf("*.*.entered should match %s", event)
		}
	}
}

func TestHooks_RunInParallelAndFailTheCallOnceAllFinished(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t,
		sdd.Hook{Event: "capture.assemble.entered", Command: "broken"},
		sdd.Hook{Event: "capture.assemble.entered", Command: "slow"},
	)
	scripted.Scripts["broken"] = proctest.HookScript{Err: errors.New("exit status 2")}
	scripted.Scripts["slow"] = proctest.HookScript{Delay: 300 * time.Millisecond}

	_, err := session.StartErr(t, "capture", nil)
	returned := time.Now()
	if err == nil || !strings.Contains(err.Error(), `hook "broken": exit status 2`) {
		t.Fatalf("a failing hook should fail the call naming it, got %v", err)
	}
	slow := scripted.RunsAt("capture.assemble.entered")
	if len(slow) != 2 || slow[1].Hook.Command != "slow" || slow[1].Finished.After(returned) {
		t.Fatalf("the sibling hook should run to completion before the call fails, runs %+v", slow)
	}

	scripted.Scripts["broken"] = proctest.HookScript{Delay: 300 * time.Millisecond}
	started := time.Now()
	session.Start(t, "capture", nil)
	if elapsed := time.Since(started); elapsed > 550*time.Millisecond {
		t.Errorf("two 300ms hooks on one event should run at once, took %s", elapsed)
	}
}

func TestHooks_TimeoutFailsTheCall(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{Event: "capture.assemble.entered", Command: "hang", Timeout: 100 * time.Millisecond})
	scripted.Scripts["hang"] = proctest.HookScript{Block: true}
	_, err := session.StartErr(t, "capture", nil)
	if err == nil || !strings.Contains(err.Error(), `hook "hang": timed out after 100ms`) {
		t.Fatalf("a hook outliving its timeout should fail the call, got %v", err)
	}
}

func TestHooks_OutputMustBeEmptyOrDeclaredFindings(t *testing.T) {
	for _, tc := range []struct {
		name, output, want string
		findings           bool
	}{
		{name: "text", output: "all good", want: `output is neither empty nor {"findings": [...]}`},
		{name: "undeclared findings", output: highFindingOutput, want: "the hook does not declare findings"},
		{name: "unknown severity", output: `{"findings": [{"severity": "blocker", "category": "x", "observation": "y"}]}`, findings: true, want: `severity "blocker" is not high, medium or low`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{Event: "capture.assemble.left", Command: "lint", Findings: tc.findings})
			scripted.Scripts["lint"] = proctest.HookScript{Output: tc.output}
			serve := session.Start(t, "capture", nil)
			_, err := session.ReportErr(t, serve.Instance, captureDraft())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the hook's answer should be refused, got %v", err)
			}
		})
	}
}

func TestHooks_ConfigurationIsCheckedAgainstTheRunningProcedures(t *testing.T) {
	err := openErr(t, sdd.Hook{Event: "capture.asemble.left", Command: "lint"})
	if err == nil || !strings.Contains(err.Error(), `unknown event "capture.asemble.left"`) || !strings.Contains(err.Error(), "capture.assemble.left") {
		t.Errorf("an unknown event should be refused listing the valid ones, got %v", err)
	}
	err = openErr(t, sdd.Hook{Event: "capture.playback.left", Command: "lint", Findings: true})
	if err == nil || !strings.Contains(err.Error(), "capture.playback.left has no findings channel") {
		t.Errorf("findings on a step without a findings channel should be refused, got %v", err)
	}
	for _, event := range []string{"capture.assemble.left", "capture.guide.left", "capture.preflight.left", "capture.verify.left"} {
		if err := openErr(t, sdd.Hook{Event: event, Command: "lint", Findings: true}); err != nil {
			t.Errorf("%s has a findings channel, refused: %v", event, err)
		}
	}
	for _, event := range []string{"capture.verify.entered", "capture.verify.round"} {
		if err := openErr(t, sdd.Hook{Event: event, Command: "status"}); err != nil {
			t.Errorf("%s is the verify step's event, refused: %v", event, err)
		}
	}
	if err := openErr(t, sdd.Hook{Event: "capture.deepRead.round", Command: "status"}); err == nil {
		t.Error("a step inside the verify stage raises no event of its own, so its name should be refused")
	}
}
