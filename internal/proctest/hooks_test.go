// Configured hooks at the steps of the shipped procedures through the real
// application.
package proctest_test

import (
	"context"
	"errors"
	"slices"
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
	instance := serve.Instance
	session.Report(t, instance, captureDraft())
	serve = session.Answer(t, instance, "playback", "confirm", nil, "capture it")
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

func TestHooks_PayloadDraftCarriesItsLifecycleAndGroundingSkip(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.preflight", Command: "status"}})
	session.LogRead(t, "show", []string{captureRef2ID}, nil)
	serve := session.Start(t, "capture", nil)
	draft := lifecycleDraft()
	draft["closes"] = []any{captureRefID}
	draft["supersedes"] = []any{captureRef2ID}
	draft["skipGrounding"] = true
	session.Report(t, serve.Instance, draft)
	session.Answer(t, serve.Instance, "playback", "confirm", nil, "capture it")

	runs := scripted.RunsAt("capture.preflight")
	if len(runs) != 1 {
		t.Fatalf("the hook should run once at capture.preflight, ran %d times", len(runs))
	}
	payload, _ := runs[0].Payload["draft"].(map[string]any)
	if !slices.Equal(payloadStrings(payload["supersedes"]), []string{captureRef2ID}) || !slices.Equal(payloadStrings(payload["closes"]), []string{captureRefID}) || payload["skip_grounding"] != true {
		t.Errorf("the draft should carry what it supersedes and closes and its grounding skip, got %v", payload)
	}
}

func payloadStrings(value any) []string {
	var out []string
	switch items := value.(type) {
	case []string:
		out = items
	case []any:
		for _, item := range items {
			text, _ := item.(string)
			out = append(out, text)
		}
	}
	return out
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
	for _, event := range []string{"capture.assemble", "capture.guide", "capture.preflight", "capture.verify"} {
		if err := openErr(t, sdd.Hook{HookConfig: model.HookConfig{Event: event, Command: "lint", Findings: true}}); err != nil {
			t.Errorf("%s has a findings channel, refused: %v", event, err)
		}
	}
	for _, event := range []string{"capture.verify.start", "capture.verify.round"} {
		if err := openErr(t, sdd.Hook{HookConfig: model.HookConfig{Event: event, Command: "status"}}); err != nil {
			t.Errorf("%s is the verify step's event, refused: %v", event, err)
		}
	}
	if err := openErr(t, sdd.Hook{HookConfig: model.HookConfig{Event: "capture.deepRead.round", Command: "status"}}); err == nil {
		t.Error("a step inside the verify stage raises no event of its own, so its name should be refused")
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
// running on every retry.
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

type answeringAllower struct {
	accept bool
	err    error
	asked  [][]sdd.HookReview
}

func (c *answeringAllower) AllowHooks(_ context.Context, reviews []sdd.HookReview) (bool, error) {
	c.asked = append(c.asked, reviews)
	return c.accept, c.err
}

func pendingHook(event, command string) sdd.Hook {
	return sdd.Hook{HookConfig: model.HookConfig{Event: event, Command: command}, State: model.HookPending,
		Origin: model.HookOrigin{Layer: model.HookLayerCommitted, ConfigPath: "/repo/.sdd/config.yaml", Checkout: "/repo"}}
}

// AllowHooks, and the session refresh that asks first, ask about every
// repository whose hooks are not allowed in one question and allow them
// only once the person accepts; without an allower, declined or not askable
// they fail naming `sdd hooks allow` and allow nothing.
func TestHooks_AllowHooksAsksAboutEveryPendingRepositoryAtOnce(t *testing.T) {
	for _, tc := range []struct {
		name      string
		allower   *answeringAllower
		bySession bool
		want      string
	}{
		{name: "no allower", want: "is pending on this machine; review and allow them with `sdd hooks allow`"},
		{name: "declined", allower: &answeringAllower{}, want: "(declined)"},
		{name: "cannot ask", allower: &answeringAllower{err: errors.New("client does not support elicitation")}, want: "client does not support elicitation"},
		{name: "accepted", allower: &answeringAllower{accept: true}},
		{name: "declined by session refresh", allower: &answeringAllower{}, bySession: true, want: "(declined)"},
		{name: "accepted by session refresh", allower: &answeringAllower{accept: true}, bySession: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world, session, scripted := newHookedCaptureWorld(t,
				pendingHook("capture.assemble.start", "notify"),
				pendingHook("entry.written", "announce"),
				sdd.Hook{HookConfig: model.HookConfig{Event: "capture.assemble.start", Command: "known"}},
			)
			var allower sdd.HookAllower
			if tc.allower != nil {
				allower = tc.allower
			}
			var err error
			if tc.bySession {
				_, err = world.App.RefreshWorkflowAllowingHooks(t.Context(), world.Identity, session.ID, allower)
			} else {
				err = world.App.AllowHooks(t.Context(), world.Identity, "", allower)
			}
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "sdd hooks allow") || !strings.Contains(err.Error(), "notify") || !strings.Contains(err.Error(), "announce") {
					t.Fatalf("the check should fail naming both pending hooks, the reason and `sdd hooks allow`, got %v", err)
				}
				if len(scripted.Allowed()) != 0 {
					t.Fatalf("a refused check should allow nothing, allowed %d", len(scripted.Allowed()))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(tc.allower.asked) != 1 || len(tc.allower.asked[0]) != 1 || len(tc.allower.asked[0][0].Hooks) != 2 {
				t.Fatalf("the person should be asked once about the one repository with both pending hooks, got %+v", tc.allower.asked)
			}
			if len(scripted.Allowed()) != 1 {
				t.Fatalf("the accepted repository should be allowed, allowed %d", len(scripted.Allowed()))
			}
			reloaded, err := world.App.RefreshWorkflow(t.Context(), world.Identity, session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reloaded.Start(t.Context(), world.Identity, sdd.WorkflowStartRequest{Canonical: "capture"}); err != nil {
				t.Fatal(err)
			}
			if runs := scripted.RunsAt("capture.assemble.start"); len(runs) != 2 {
				t.Errorf("the allowed hooks should run, ran %d", len(runs))
			}
		})
	}
}

// A hook still not allowed when the call fires it, because it changed after
// the call's check, fails the call naming `sdd hooks allow` and does not run.
func TestHooks_NotAllowedHookAtItsEventFailsTheCall(t *testing.T) {
	_, session, scripted := newHookedCaptureWorld(t, pendingHook("capture.assemble.start", "notify"))
	_, err := session.WF.Start(t.Context(), session.World.Identity, sdd.WorkflowStartRequest{Canonical: "capture"})
	if err == nil || !strings.Contains(err.Error(), "is pending on this machine; review it and allow it with `sdd hooks allow`") || !strings.Contains(err.Error(), "/repo/.sdd/config.yaml") {
		t.Fatalf("the call should fail naming the hook's file and `sdd hooks allow`, got %v", err)
	}
	if runs := scripted.RunsAt("capture.assemble.start"); len(runs) != 0 {
		t.Fatalf("a hook not allowed should not run, ran %d", len(runs))
	}
}
