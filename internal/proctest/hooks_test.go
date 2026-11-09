// Configured hooks at the steps of the shipped procedures through the real
// application (20261109-213250-d-tac-bve).
package proctest_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/pkg/llm"

	"github.com/networkteam/sdd/internal/model"
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
		sdd.Hook{HookConfig: model.HookConfig{Event: "capture.preflight", Command: "lint", Findings: true}},
		sdd.Hook{HookConfig: model.HookConfig{Event: "entry.written", Command: "notify"}},
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
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.assemble", Command: "lint", Findings: true}})
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
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.guide", Command: "lint", Findings: true}})
	scripted.Scripts["lint"] = proctest.HookScript{Output: highFindingOutput}

	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, captureDraft())
	proctest.RequireStep(t, serve, "guide")
	if !hasDiagnostic(serve, "the opening sentence is passive") {
		t.Fatalf("the held draft should name the hook's finding, diagnostics %v", serve.Diagnostics)
	}
}

func TestHooks_PayloadCarriesTheStepTheDraftAndItsFindings(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.preflight", Command: "status"}})

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Report(t, instance, captureDraft())
	session.Answer(t, instance, "playback", "confirm", nil, "capture it")

	runs := scripted.RunsAt("capture.preflight")
	if len(runs) != 1 {
		t.Fatalf("the hook should run once at capture.preflight, ran %d times", len(runs))
	}
	payload := runs[0].Payload
	for field, want := range map[string]any{
		"procedure": "capture", "instance": instance, "step": "preflight", "phase": "done",
		"transition": "write", "project": "proctest", "branch": "main", "session": string(session.ID),
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
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{HookConfig: model.HookConfig{Event: "*.*.start", Command: "status"}})
	session.Start(t, "capture", nil)
	for _, event := range []string{"user-dialogue.junction.start", "capture.assemble.start"} {
		if len(scripted.RunsAt(event)) != 1 {
			t.Errorf("*.*.start should match %s", event)
		}
	}
}

func TestHooks_RunInParallelAndFailTheCallOnceAllFinished(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t,
		sdd.Hook{HookConfig: model.HookConfig{Event: "capture.assemble.start", Command: "broken"}},
		sdd.Hook{HookConfig: model.HookConfig{Event: "capture.assemble.start", Command: "slow"}},
	)
	scripted.Scripts["broken"] = proctest.HookScript{Err: errors.New("exit status 2")}
	scripted.Scripts["slow"] = proctest.HookScript{Delay: 300 * time.Millisecond}

	_, err := session.StartErr(t, "capture", nil)
	returned := time.Now()
	if err == nil || !strings.Contains(err.Error(), "hook broken (") || !strings.Contains(err.Error(), "exit status 2") {
		t.Fatalf("a failing hook should fail the call naming it, got %v", err)
	}
	slow := scripted.RunsAt("capture.assemble.start")
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
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.assemble.start", Command: "hang", Timeout: 1}})
	scripted.Scripts["hang"] = proctest.HookScript{Block: true}
	_, err := session.StartErr(t, "capture", nil)
	if err == nil || !strings.Contains(err.Error(), "hook hang (") || !strings.Contains(err.Error(), "timed out after 1s") {
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
			_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.assemble", Command: "lint", Findings: tc.findings}})
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
	err := openErr(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.asemble", Command: "lint"}})
	if err == nil || !strings.Contains(err.Error(), `unknown event "capture.asemble"`) || !strings.Contains(err.Error(), "capture.assemble") {
		t.Errorf("an unknown event should be refused listing the valid ones, got %v", err)
	}
	err = openErr(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.playback", Command: "lint", Findings: true}})
	if err == nil || !strings.Contains(err.Error(), "capture.playback has no findings channel") {
		t.Errorf("findings on a step without a findings channel should be refused, got %v", err)
	}
	for _, event := range []string{"capture.assemble", "capture.guide", "capture.preflight"} {
		if err := openErr(t, sdd.Hook{HookConfig: model.HookConfig{Event: event, Command: "lint", Findings: true}}); err != nil {
			t.Errorf("%s has a findings channel, refused: %v", event, err)
		}
	}
	if err := openErr(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.verify.round", Command: "status"}}); err == nil {
		t.Error("capture has no verify step, so its round event should be refused")
	}
}

func TestHooks_DoneAliasMatchesTheBareStepEvent(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.assemble.done", Command: "lint", Findings: true}})
	scripted.Scripts["lint"] = proctest.HookScript{Output: highFindingOutput}

	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, captureDraft())
	proctest.RequireStep(t, serve, "assemble")
	if !hasDiagnostic(serve, "the opening sentence is passive") {
		t.Fatalf("a hook on capture.assemble.done should join capture.assemble's findings, diagnostics %v", serve.Diagnostics)
	}
}

// A failing start hook on pre-flight keeps the pre-flight LLM call from
// running on every retry (20261109-213250-d-tac-bve).
func TestHooks_FailingStartHookKeepsThePreflightCallFromRunning(t *testing.T) {
	world, session, scripted := newHookedCaptureWorld(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.preflight.start", Command: "gate"}})
	scripted.Scripts["gate"] = proctest.HookScript{Err: errors.New("exit status 1")}

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Report(t, instance, captureDraft())
	if _, err := session.AnswerErr(t, instance, "playback", "confirm", nil, "capture it"); err == nil || !strings.Contains(err.Error(), "step preflight is held until its start hooks pass") {
		t.Fatalf("a failing start hook should fail the call naming the held step, got %v", err)
	}
	if _, err := session.ReportErr(t, instance, captureDraft()); err == nil || !strings.Contains(err.Error(), "held") {
		t.Fatalf("the retry should stay held, got %v", err)
	}
	if calls := world.LLM.Calls(llm.PurposePreflight); calls != 0 {
		t.Fatalf("a held pre-flight should make no LLM call, made %d", calls)
	}
	if runs := len(scripted.RunsAt("capture.preflight.start")); runs != 2 {
		t.Errorf("the start hook should run on every call, ran %d times", runs)
	}

	scripted.Scripts["gate"] = proctest.HookScript{}
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "verifySummary")
	if calls := world.LLM.Calls(llm.PurposePreflight); calls != 1 {
		t.Errorf("once the start hook passes pre-flight should run once, ran %d times", calls)
	}
}

type answeringConfirmer struct {
	accept bool
	err    error
	asked  [][]sdd.HookReview
}

func (c *answeringConfirmer) ConfirmHooks(_ context.Context, reviews []sdd.HookReview) (bool, error) {
	c.asked = append(c.asked, reviews)
	return c.accept, c.err
}

func pendingHook(event, command string) sdd.Hook {
	return sdd.Hook{HookConfig: model.HookConfig{Event: event, Command: command}, State: model.HookPending,
		Origin: model.HookOrigin{Layer: model.HookLayerCommitted, ConfigPath: "/repo/.sdd/config.yaml"}}
}

// ConfirmHooks, and the session refresh that asks first, ask about every
// pending hook of the project in one question and confirm them all only once
// the person accepts; without a confirmer, declined or not askable they fail
// naming `sdd hooks trust` and confirm nothing (20261109-213250-d-tac-bve).
func TestHooks_ConfirmHooksAsksAboutEveryPendingHookAtOnce(t *testing.T) {
	for _, tc := range []struct {
		name      string
		confirmer *answeringConfirmer
		bySession bool
		want      string
	}{
		{name: "no confirmer", want: "is pending on this machine; review and confirm with `sdd hooks trust`"},
		{name: "declined", confirmer: &answeringConfirmer{}, want: "(declined)"},
		{name: "cannot ask", confirmer: &answeringConfirmer{err: errors.New("client does not support elicitation")}, want: "client does not support elicitation"},
		{name: "accepted", confirmer: &answeringConfirmer{accept: true}},
		{name: "declined by session refresh", confirmer: &answeringConfirmer{}, bySession: true, want: "(declined)"},
		{name: "accepted by session refresh", confirmer: &answeringConfirmer{accept: true}, bySession: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world, session, scripted := newHookedCaptureWorld(t,
				pendingHook("capture.assemble.start", "notify"),
				pendingHook("entry.written", "announce"),
				sdd.Hook{HookConfig: model.HookConfig{Event: "capture.assemble.start", Command: "known"}},
			)
			var confirmer sdd.HookConfirmer
			if tc.confirmer != nil {
				confirmer = tc.confirmer
			}
			var err error
			if tc.bySession {
				_, err = world.App.RefreshWorkflowConfirmingHooks(t.Context(), world.Identity, session.ID, confirmer)
			} else {
				err = world.App.ConfirmHooks(t.Context(), world.Identity, "", confirmer)
			}
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "sdd hooks trust") || !strings.Contains(err.Error(), "notify") || !strings.Contains(err.Error(), "announce") {
					t.Fatalf("the check should fail naming both pending hooks, the reason and `sdd hooks trust`, got %v", err)
				}
				if len(scripted.Confirmed()) != 0 {
					t.Fatalf("a refused check should confirm nothing, confirmed %d", len(scripted.Confirmed()))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(tc.confirmer.asked) != 1 || len(tc.confirmer.asked[0]) != 2 || tc.confirmer.asked[0][0].Hook.Command != "notify" || tc.confirmer.asked[0][1].Hook.Command != "announce" {
				t.Fatalf("the person should be asked once about both pending hooks, got %+v", tc.confirmer.asked)
			}
			if len(scripted.Confirmed()) != 2 {
				t.Fatalf("both accepted hooks should be confirmed, confirmed %d", len(scripted.Confirmed()))
			}
			reloaded, err := world.App.RefreshWorkflow(t.Context(), world.Identity, session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reloaded.Start(t.Context(), world.Identity, sdd.WorkflowStartRequest{Canonical: "capture"}); err != nil {
				t.Fatal(err)
			}
			if runs := scripted.RunsAt("capture.assemble.start"); len(runs) != 2 {
				t.Errorf("the confirmed hooks should run, ran %d", len(runs))
			}
		})
	}
}

// A hook still unconfirmed when the call fires it, because it changed after
// the call's check, fails the call naming `sdd hooks trust` and does not run.
func TestHooks_UnconfirmedHookAtItsEventFailsTheCall(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, pendingHook("capture.assemble.start", "notify"))
	_, err := session.WF.Start(t.Context(), session.World.Identity, sdd.WorkflowStartRequest{Canonical: "capture"})
	if err == nil || !strings.Contains(err.Error(), "is pending on this machine; review and confirm it with `sdd hooks trust`") || !strings.Contains(err.Error(), "/repo/.sdd/config.yaml") {
		t.Fatalf("the call should fail naming the hook's file and `sdd hooks trust`, got %v", err)
	}
	if runs := scripted.RunsAt("capture.assemble.start"); len(runs) != 0 {
		t.Fatalf("an unconfirmed hook should not run, ran %d", len(runs))
	}
}
