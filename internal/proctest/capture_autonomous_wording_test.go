package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// No user reads what an autonomous capture serves the agent, so what a guided
// capture tells the user goes to the agent's own check or its report; a guided
// capture keeps its wording for the user, apart from check findings, which
// reach the user only at the playback.

type wordingMode struct {
	name     string
	options  []proctest.Option
	guided   bool
	want     []string
	unwanted []string
}

func requireWording(t *testing.T, serve *sdd.WorkflowServe, mode wordingMode) {
	t.Helper()
	text := serve.Instructions
	for _, want := range mode.want {
		if !strings.Contains(text, want) {
			t.Errorf("the %s capture should carry %q:\n%s", mode.name, want, text)
		}
	}
	for _, unwanted := range mode.unwanted {
		if strings.Contains(text, unwanted) {
			t.Errorf("the %s capture should not carry %q:\n%s", mode.name, unwanted, text)
		}
	}
}

func TestCaptureWording_SummaryCheckNamesTheDeepFindings(t *testing.T) {
	const (
		toldWithdrawn = "Tell the user which findings the verifier withdrew and why."
		toldUnedited  = "Tell the user each one."
		ownWithdrawn  = "Name them in what you report, each with its reason."
		ownUnedited   = "Name each in what you report."
	)
	for _, mode := range []wordingMode{
		{name: "autonomous", options: []proctest.Option{proctest.WithAutonomousDialogue(), clientActor("deep-summary-autonomous")}, want: []string{ownWithdrawn, ownUnedited}, unwanted: []string{"Tell the user"}},
		{name: "assisted", options: []proctest.Option{proctest.WithAssistedDialogue(), clientActor("deep-summary-assisted")}, unwanted: []string{"Tell the user", ownWithdrawn, ownUnedited, "withdrew", "which carried no edit"}},
		{name: "guided", guided: true, unwanted: []string{"Tell the user", ownWithdrawn, ownUnedited, "withdrew", "which carried no edit"}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			world, session := newDeepWorld(t, "deep-summary-"+mode.name, sdd.VerifierPolicy{}, mode.options...)
			var x *deepExchange
			if mode.guided {
				x = startDeep(t, world, session, captureDraft())
			} else {
				serve := session.Start(t, "capture", nil)
				session.Gather(t, serve.Instance)
				serve = session.Report(t, serve.Instance, captureDraft())
				proctest.RequireStep(t, serve, "deepDispatch")
				x = dispatchDeep(t, world, session, serve.Instance)
			}
			x.judge(t, []any{
				map[string]any{"verdict": "the gap rests on incidents no entry records", "axis": "missing-ref", "severity": "medium"},
				map[string]any{"verdict": "the layer is too low", "axis": "ref-fit", "severity": "low"},
			})
			x.pickUp(t)
			session.Answer(t, x.instance, "deepAnswer", "contest", map[string]any{"deepContest": []any{
				map[string]any{"target": "f1", "verdict": "contest", "reason": "the user named the incidents in the dialogue"},
			}}, "")
			x.delegate.Refresh(t)
			x.delegate.Report(t, x.child, nil)
			x.delegate.Answer(t, x.child, "respond", "respond", map[string]any{"responses": []any{
				map[string]any{"target": "f1", "outcome": "conceded", "note": "the dialogue carries the incidents"},
			}}, "")
			session.Refresh(t)
			proctest.RequireStep(t, session.Report(t, x.instance, nil), "deepProposal")
			serve := session.Answer(t, x.instance, "deepProposal", "accept", nil, "")
			switch mode.name {
			case "autonomous":
				proctest.RequireStep(t, serve, "playback")
				serve = session.Decide(t, x.instance, "playback", "confirm", nil, "the draft holds")
			case "assisted":
				proctest.RequireStep(t, serve, "playback")
				serve = session.Answer(t, x.instance, "playback", "confirm", nil, "fine as it is")
			}
			proctest.RequireStep(t, serve, "verifySummary")
			requireWording(t, serve, mode)
		})
	}
}

func TestCaptureWording_GuideReviewNamesSubstantiveFindings(t *testing.T) {
	for _, mode := range []wordingMode{
		{name: "autonomous", options: []proctest.Option{proctest.WithAutonomousDialogue(), clientActor("guide-wording-autonomous")}, want: []string{"Name each `substantive` finding you act on or dismiss in what you report."}, unwanted: []string{"to the user in your own words"}},
		{name: "guided", guided: true, want: []string{"Name each `substantive` finding you act on or dismiss in what you report.", "Write in only what the user said; otherwise dismiss it."}, unwanted: []string{"to the user in your own words", "ask the user"}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			world, session := newCaptureWorld(t, "guide-wording-"+mode.name, mode.options...)
			world.LLM.GuideFindings = []proctest.GuideFinding{{
				Reasoning: "the body leans on 'the earlier approach' without naming it",
				Axis:      "stranding", Quote: "the earlier approach", Repair: "write-in", Severity: "substantive",
			}}
			serve := session.Start(t, "capture", nil)
			serve = session.Report(t, serve.Instance, captureDraft())
			if mode.guided {
				proctest.RequireStep(t, serve, "playback")
				serve = session.Answer(t, serve.Instance, "playback", "confirm", nil, "looks right")
			}
			proctest.RequireStep(t, serve, "guideReview")
			requireWording(t, serve, mode)
		})
	}
}

func TestCaptureWording_LightVerdictNamesSetAsideFindings(t *testing.T) {
	for _, mode := range []wordingMode{
		{name: "autonomous", options: []proctest.Option{proctest.WithAutonomousDialogue(), clientActor("light-wording-autonomous")}, want: []string{"Set aside a finding only a person can settle.", "Name each `medium` finding you set aside in what you report."}, unwanted: []string{"to the user in your own words", "Ask the user"}},
		{name: "guided", guided: true, want: []string{"Set aside a finding only a person can settle.", "Name each `medium` finding you set aside in what you report."}, unwanted: []string{"to the user in your own words", "Ask the user"}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			world, session := newCaptureWorld(t, "light-wording-"+mode.name, append([]proctest.Option{proctest.WithGroundingEnforced(),
				proctest.WithVerifier(sdd.VerifierPolicy{Mode: "prose"}, 200000, 4096)}, mode.options...)...)
			world.LLM.PreflightFindings = []proctest.PreflightFinding{{Severity: "medium", Category: "outdated", Observation: "the draft leans on a closed gap"}}
			serve := session.Start(t, "capture", nil)
			instance := serve.Instance
			session.Gather(t, instance, captureRef2ID)
			draft := captureDraft()
			draft["verdicts"] = []any{map[string]any{"ids": []any{captureRef2ID}, "code": model.VerdictCovered}}
			serve = session.Report(t, instance, draft)
			if mode.guided {
				proctest.RequireStep(t, serve, "playback")
				serve = session.Answer(t, instance, "playback", "confirm", nil, "write it")
			}
			proctest.RequireStep(t, serve, "preflightVerdict")
			requireWording(t, serve, mode)
		})
	}
}

func TestCaptureWording_VerifyHookReviewNamesSetAsideFindings(t *testing.T) {
	for _, mode := range []wordingMode{
		{name: "autonomous", options: []proctest.Option{proctest.WithAutonomousDialogue(), clientActor("verify-hook-wording-autonomous")}, want: []string{"Name each `medium` finding you set aside in what you report."}, unwanted: []string{"to the user in your own words"}},
		{name: "guided", guided: true, want: []string{"Name each `medium` finding you set aside in what you report."}, unwanted: []string{"to the user in your own words"}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			scripted := &proctest.ScriptedHooks{
				Configured: []sdd.Hook{{HookConfig: model.HookConfig{Event: "capture.verify", Command: "lint", Findings: true}}},
				Scripts:    map[string]proctest.HookScript{"lint": {Output: mediumFindingOutput}},
			}
			world, session := newDeepWorld(t, "verify-hook-wording-"+mode.name, sdd.VerifierPolicy{}, append([]proctest.Option{proctest.WithHooks(scripted)}, mode.options...)...)
			var x *deepExchange
			if mode.guided {
				x = startDeep(t, world, session, captureDraft())
			} else {
				serve := session.Start(t, "capture", nil)
				session.Gather(t, serve.Instance)
				serve = session.Report(t, serve.Instance, captureDraft())
				proctest.RequireStep(t, serve, "deepDispatch")
				x = dispatchDeep(t, world, session, serve.Instance)
			}
			x.judge(t, []any{})
			serve := x.pickUp(t)
			requireServedHookFinding(t, serve, "verifyHookReview")
			requireWording(t, serve, mode)
		})
	}
}

func TestCaptureWording_PlaybackTellsTheUserOnlyWhenGuided(t *testing.T) {
	told := []string{
		"Compose what you present to the user from what you hold plus what the block confirms.",
		"the entry is written with the local participant alone, so name that person to the user.",
		"whatever confidence the block shows. Tell the user before they confirm.",
	}
	for _, mode := range []wordingMode{
		{name: "autonomous", options: []proctest.Option{proctest.WithAutonomousDialogue(), clientActor("playback-wording-autonomous")}, want: []string{"the entry is written with the local participant alone.", "whatever confidence the block shows."}, unwanted: append([]string{"Tell the user", "to the user"}, told...)},
		{name: "guided", guided: true, want: told},
	} {
		t.Run(mode.name, func(t *testing.T) {
			_, session := newCaptureWorld(t, "playback-wording-"+mode.name, mode.options...)
			serve := session.Start(t, "capture", nil)
			serve = session.Report(t, serve.Instance, captureDraft())
			proctest.RequireStep(t, serve, "playback")
			requireWording(t, serve, mode)
		})
	}
}
