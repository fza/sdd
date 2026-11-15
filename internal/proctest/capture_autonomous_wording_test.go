package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/test"
)

// No user reads what an autonomous capture serves the agent, so what a guided
// capture tells the user goes to the agent's own check or its report; a guided
// capture keeps its wording for the user.

type wordingMode struct {
	name     string
	options  []proctest.Option
	guided   bool
	want     []string
	unwanted []string
}

func requireWording(t *testing.T, serve *sdd.WorkflowServe, mode wordingMode) {
	t.Helper()
	text := test.Untagged(serve.Instructions)
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
		{name: "autonomous", options: []proctest.Option{proctest.WithAutonomousDialogue()}, want: []string{ownWithdrawn, ownUnedited}, unwanted: []string{"Tell the user"}},
		{name: "guided", guided: true, want: []string{toldWithdrawn, toldUnedited}, unwanted: []string{"in what you report"}},
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
			serve := session.Report(t, x.instance, nil)
			proctest.RequireStep(t, serve, "playback")
			if mode.guided {
				serve = session.Answer(t, x.instance, "playback", "confirm", nil, "fine as it is")
			} else {
				serve = session.Decide(t, x.instance, "playback", "confirm", nil, "the draft holds")
			}
			proctest.RequireStep(t, serve, "verifySummary")
			requireWording(t, serve, mode)
		})
	}
}
