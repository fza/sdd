// With a person present, a capture shows the checks' outcome once, at the
// final playback (20261123-100216-d-tac-b49).
package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/llm"
)

const (
	heldObservation = "blocked until the person settles it"
	heldLine        = "- pre-flight [high] test-block: " + heldObservation
	ownerLine       = "- f1 [medium, aspiration]: only the user can settle it"
	heldBody        = "A tactical gap: the fixture observes something in scope."
)

// ownerPoint is a deep finding only the user can settle.
func ownerPoint() []any {
	return []any{map[string]any{
		"verdict": "whether this gap is in scope is the owner's call", "axis": "aspiration", "severity": "medium", "owner": true,
		"pointers": []any{map[string]any{"field": "body", "quote": "observes something"}},
	}}
}

// personMode is a dialogue mode in which a person sees the playback.
type personMode struct {
	name    string
	guided  bool
	options func(connID string) []proctest.Option
}

var personModes = []personMode{
	{name: "guided", guided: true, options: func(string) []proctest.Option { return nil }},
	{name: "assisted", options: func(connID string) []proctest.Option {
		return []proctest.Option{proctest.WithAssistedDialogue(), clientActor(connID)}
	}},
}

// checkedDraft reports the draft and, in guided mode, confirms its first
// playback, so the checks run.
func (m personMode) checkedDraft(t *testing.T, session *proctest.Session, draft map[string]any) (string, *sdd.WorkflowServe) {
	t.Helper()
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	serve = session.Report(t, instance, draft)
	if m.guided {
		proctest.RequireStep(t, serve, "playback")
		serve = session.Answer(t, instance, "playback", "confirm", nil, "write it")
	}
	return instance, serve
}

// deepExchange reaches the deep exchange's dispatch and forks the verifier.
func (m personMode) deepExchange(t *testing.T, world *proctest.World, session *proctest.Session) *deepExchange {
	t.Helper()
	instance, serve := m.checkedDraft(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	return dispatchDeep(t, world, session, instance)
}

func heldPreflightWorld(t *testing.T, connID string, options ...proctest.Option) (*proctest.World, *proctest.Session) {
	t.Helper()
	world, session := newCaptureWorld(t, connID, options...)
	world.LLM.PreflightFindings = []proctest.PreflightFinding{{Severity: "high", Category: "test-block", Observation: heldObservation}}
	return world, session
}

// heldPreflight drives a capture to the pre-flight finding the agent judges
// wrong and holds it for the person, returning the playback that follows.
func (m personMode) heldPreflight(t *testing.T, session *proctest.Session) (string, *sdd.WorkflowServe) {
	t.Helper()
	instance, serve := m.checkedDraft(t, session, captureDraft())
	proctest.RequireStep(t, serve, "preflightRoute")
	if serve.PendingChooser == nil || serve.PendingChooser.Kind != "agent" {
		t.Fatalf("a high pre-flight finding should be the agent's to route, got %+v", serve.PendingChooser)
	}
	if !strings.Contains(serve.Instructions, heldObservation) {
		t.Fatalf("the routing step should serve the finding:\n%s", serve.Instructions)
	}
	return instance, session.Answer(t, instance, "preflightRoute", "hold", nil, "")
}

// holdAndConfirm holds the blocking pre-flight findings for the user and
// answers the playback that shows them with the user's confirm, which
// overrides them.
func holdAndConfirm(t *testing.T, session *proctest.Session, instance, userWords string) *sdd.WorkflowServe {
	t.Helper()
	proctest.RequireStep(t, session.Answer(t, instance, "preflightRoute", "hold", nil, ""), "playback")
	return session.Answer(t, instance, "playback", "confirm", nil, userWords)
}

func requireOpenPoint(t *testing.T, serve *sdd.WorkflowServe, line string) {
	t.Helper()
	proctest.RequireStep(t, serve, "playback")
	if serve.Parked != nil && serve.Parked.Step != "playback" {
		t.Fatalf("an open point should not park the capture before the playback, parked %+v", serve.Parked)
	}
	for _, want := range []string{"Open for you", line + "\n", "what the draft holds, and what the checker holds"} {
		if !strings.Contains(serve.Instructions, want) {
			t.Fatalf("the playback should carry %q:\n%s", want, serve.Instructions)
		}
	}
}

func TestSinglePlayback_APreflightFindingTheAgentJudgesWrongIsOpenAtThePlayback(t *testing.T) {
	for _, mode := range personModes {
		t.Run(mode.name, func(t *testing.T) {
			connID := "held-preflight-" + mode.name
			world, session := heldPreflightWorld(t, connID, mode.options(connID)...)
			instance, serve := mode.heldPreflight(t, session)
			requireOpenPoint(t, serve, heldLine)
			entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
				return session.Answer(t, instance, "playback", "confirm", nil, "the draft stands")
			})
			if entry.Preflight != model.PreflightSkipped {
				t.Errorf("the confirm should record the person's override of the held finding, got preflight %q", entry.Preflight)
			}
			if calls := world.LLM.Calls(llm.PurposePreflight); calls != 1 {
				t.Errorf("the override should write without a second pre-flight, got %d calls", calls)
			}
		})
	}
}

func TestSinglePlayback_AnOwnerPointIsOpenAtThePlayback(t *testing.T) {
	for _, mode := range personModes {
		t.Run(mode.name, func(t *testing.T) {
			connID := "held-owner-" + mode.name
			world, session := newDeepWorld(t, connID, sdd.VerifierPolicy{}, mode.options(connID)...)
			x := mode.deepExchange(t, world, session)
			x.judge(t, ownerPoint())
			proctest.RequireStep(t, x.pickUp(t), "deepAnswer")
			proctest.RequireStep(t, session.Answer(t, x.instance, "deepAnswer", "accept", nil, ""), "deepProposal")
			serve := session.Answer(t, x.instance, "deepProposal", "accept", nil, "")
			requireOpenPoint(t, serve, ownerLine)
			writeCapture(t, world, session, x.instance, func() *sdd.WorkflowServe {
				return session.Answer(t, x.instance, "playback", "confirm", nil, "it is in scope")
			})
		})
	}
}

func TestSinglePlayback_AnEscalatedPointIsOpenAtThePlayback(t *testing.T) {
	for _, mode := range personModes {
		t.Run(mode.name, func(t *testing.T) {
			connID := "held-escalate-" + mode.name
			world, session := newDeepWorld(t, connID, sdd.VerifierPolicy{}, mode.options(connID)...)
			x := mode.deepExchange(t, world, session)
			x.judge(t, ownerPoint())
			proctest.RequireStep(t, x.pickUp(t), "deepAnswer")
			session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
			serve := session.Answer(t, x.instance, "deepProposal", "escalate", nil, "")
			requireOpenPoint(t, serve, ownerLine)
			writeCapture(t, world, session, x.instance, func() *sdd.WorkflowServe {
				return session.Answer(t, x.instance, "playback", "confirm", nil, "it is in scope")
			})
		})
	}
}

func TestSinglePlayback_AnAdjustRunsTheChecksAgain(t *testing.T) {
	for _, mode := range personModes {
		t.Run(mode.name+" pre-flight", func(t *testing.T) {
			connID := "held-adjust-" + mode.name
			world, session := heldPreflightWorld(t, connID, mode.options(connID)...)
			instance, serve := mode.heldPreflight(t, session)
			requireOpenPoint(t, serve, heldLine)
			world.LLM.PreflightFindings = nil
			serve = session.Answer(t, instance, "playback", "adjust", map[string]any{"body": "A tactical gap: the fixture observes something else."}, "say something else")
			proctest.RequireStep(t, serve, "playback")
			if strings.Contains(serve.Instructions, "Open for you") {
				t.Errorf("a point the adjust answered should not stay open:\n%s", serve.Instructions)
			}
			if strings.Contains(serve.Instructions, "Changed by the checks") {
				t.Errorf("the user's adjust is not listed as changed by the checks:\n%s", serve.Instructions)
			}
			entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
				return session.Answer(t, instance, "playback", "confirm", nil, "now write it")
			})
			if calls := world.LLM.Calls(llm.PurposePreflight); calls != 2 {
				t.Errorf("the adjusted draft should go through pre-flight again, got %d calls", calls)
			}
			if entry.Preflight != "" {
				t.Errorf("an adjusted draft that passes pre-flight records no override, got preflight %q", entry.Preflight)
			}
		})
		t.Run(mode.name+" deep", func(t *testing.T) {
			connID := "held-adjust-deep-" + mode.name
			world, session := newDeepWorld(t, connID, sdd.VerifierPolicy{}, mode.options(connID)...)
			x := mode.deepExchange(t, world, session)
			x.judge(t, ownerPoint())
			proctest.RequireStep(t, x.pickUp(t), "deepAnswer")
			session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
			requireOpenPoint(t, session.Answer(t, x.instance, "deepProposal", "accept", nil, ""), ownerLine)
			serve := session.Answer(t, x.instance, "playback", "adjust", map[string]any{"body": heldBody}, "say it is in scope")
			if mode.guided {
				proctest.RequireStep(t, serve, "playback")
				serve = session.Answer(t, x.instance, "playback", "confirm", nil, "now write it")
			}
			proctest.RequireStep(t, serve, "deepWait")
			x.pickUpRevision(t)
		})
	}
}

func TestSinglePlayback_AFindingSettledWithoutAnEditIsNotShown(t *testing.T) {
	const settled = "accepted without an edit"
	for _, mode := range personModes {
		t.Run(mode.name, func(t *testing.T) {
			connID := "unedited-hidden-" + mode.name
			world, session := newDeepWorld(t, connID, sdd.VerifierPolicy{}, mode.options(connID)...)
			x := mode.deepExchange(t, world, session)
			x.judge(t, []any{map[string]any{"verdict": "the gap claims an observation the graph records nowhere", "axis": "inconsistent", "severity": "medium"}})
			proctest.RequireStep(t, x.pickUp(t), "deepAnswer")
			proctest.RequireStep(t, session.Answer(t, x.instance, "deepAnswer", "accept", nil, ""), "deepProposal")
			serve := session.Answer(t, x.instance, "deepProposal", "accept", nil, "")
			if !mode.guided {
				proctest.RequireStep(t, serve, "playback")
				if strings.Contains(serve.Instructions, settled) || strings.Contains(serve.Instructions, "without a change to the draft") {
					t.Errorf("the playback should not show a finding settled without an edit:\n%s", serve.Instructions)
				}
				serve = session.Answer(t, x.instance, "playback", "confirm", nil, "fine as it is")
			}
			proctest.RequireStep(t, serve, "verifySummary")
			if strings.Contains(serve.Instructions, settled) || strings.Contains(serve.Instructions, "which carried no edit") {
				t.Errorf("the summary check should not name a finding settled without an edit:\n%s", serve.Instructions)
			}
		})
	}
}

func TestSinglePlayback_ThePlaybackShowsTheEntryFileAndWhatTheChecksChanged(t *testing.T) {
	const added = `- "It names the subject."`
	for _, mode := range personModes {
		t.Run(mode.name, func(t *testing.T) {
			connID := "checks-changed-" + mode.name
			world, session := newDeepWorld(t, connID, sdd.VerifierPolicy{}, mode.options(connID)...)
			x := mode.deepExchange(t, world, session)
			x.judge(t, subjectFinding("observes something."))
			proctest.RequireStep(t, x.pickUp(t), "deepAnswer")
			proctest.RequireStep(t, session.Answer(t, x.instance, "deepAnswer", "accept", nil, ""), "deepProposal")
			serve := session.Answer(t, x.instance, "deepProposal", "accept", nil, "")
			proctest.RequireStep(t, serve, "playback")
			for _, want := range []string{"the draft as the entry file sdd writes: its YAML frontmatter first", "Changed by the checks", added} {
				if !strings.Contains(serve.Instructions, want) {
					t.Errorf("the playback should carry %q:\n%s", want, serve.Instructions)
				}
			}
			if strings.Contains(serve.Instructions, "Open for you") {
				t.Errorf("a playback with no open point should list none:\n%s", serve.Instructions)
			}
			serve = session.Answer(t, x.instance, "playback", "confirm", nil, "fine")
			proctest.RequireStep(t, serve, "verifySummary")
		})
	}
}

// A guided round after a confirm lists only what the checks changed since.
func TestSinglePlayback_AConfirmEndsTheChangedList(t *testing.T) {
	world, session := newCaptureWorld(t, "checks-changed-once")
	world.LLM.GuideFindings = []proctest.GuideFinding{{
		Reasoning: "the body names no reason the gap matters", Axis: "stranding",
		Quote: "the fixture observes something", Repair: "write-in", Severity: "substantive",
	}}
	world.LLM.PreflightFindings = []proctest.PreflightFinding{{Severity: "high", Category: "test-block", Observation: heldObservation}}
	instance, serve := personModes[0].checkedDraft(t, session, captureDraft())
	proctest.RequireStep(t, serve, "guideReview")
	serve = session.Answer(t, instance, "guideReview", "revise", map[string]any{"body": "A tactical gap: the fixture observes something.\n\nIt blocks the release."}, "")
	proctest.RequireStep(t, serve, "playback")
	if !strings.Contains(serve.Instructions, `- "It blocks the release."`) {
		t.Fatalf("the playback after the guide's revision should list its sentence:\n%s", serve.Instructions)
	}
	serve = session.Answer(t, instance, "playback", "confirm", nil, "yes")
	proctest.RequireStep(t, serve, "preflightRoute")
	serve = session.Answer(t, instance, "preflightRoute", "hold", nil, "")
	requireOpenPoint(t, serve, heldLine)
	if strings.Contains(serve.Instructions, "Changed by the checks") {
		t.Errorf("a round after the confirm should not list what the person already confirmed:\n%s", serve.Instructions)
	}
}

// A point held for the person goes back to the choice it came from when the
// session moved to auto mode before the person confirmed: the agent's
// confirm never settles it.
func TestSinglePlayback_AnAgentsConfirmNeverSettlesAnOpenPoint(t *testing.T) {
	t.Run("pre-flight", func(t *testing.T) {
		const connID = "held-to-auto"
		world, session := heldPreflightWorld(t, connID, personModes[1].options(connID)...)
		instance, serve := personModes[1].heldPreflight(t, session)
		requireOpenPoint(t, serve, heldLine)
		world.SetDialogue("auto")
		resumed, _ := world.Resume(t, session.ID, connID)
		serve = resumed.Decide(t, instance, "playback", "confirm", nil, "the draft holds")
		proctest.RequireStep(t, serve, "reviseOrOverride")
		if calls := world.LLM.Calls(llm.PurposePreflight); calls != 2 {
			t.Errorf("the held finding should go back through pre-flight, got %d calls", calls)
		}
	})
	t.Run("deep", func(t *testing.T) {
		const connID = "held-deep-to-auto"
		world, session := newDeepWorld(t, connID, sdd.VerifierPolicy{}, personModes[1].options(connID)...)
		x := personModes[1].deepExchange(t, world, session)
		x.judge(t, ownerPoint())
		proctest.RequireStep(t, x.pickUp(t), "deepAnswer")
		session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
		requireOpenPoint(t, session.Answer(t, x.instance, "deepProposal", "accept", nil, ""), ownerLine)
		world.SetDialogue("auto")
		resumed, _ := world.Resume(t, session.ID, connID)
		serve := resumed.Decide(t, x.instance, "playback", "confirm", nil, "the draft holds")
		proctest.RequireStep(t, serve, "deepUser")
	})
}

func TestSinglePlayback_AutoModeServesTheCaptureAsBefore(t *testing.T) {
	t.Run("pre-flight", func(t *testing.T) {
		_, session := heldPreflightWorld(t, "auto-preflight", proctest.WithAutonomousDialogue(), clientActor("auto-preflight"))
		serve := session.Start(t, "capture", nil)
		serve = session.Report(t, serve.Instance, captureDraft())
		proctest.RequireStep(t, serve, "playback")
		serve = session.Decide(t, serve.Instance, "playback", "confirm", nil, "the draft holds")
		proctest.RequireStep(t, serve, "reviseOrOverride")
		if serve.PendingChooser == nil || serve.PendingChooser.Kind != "user" {
			t.Fatalf("in auto mode a high finding stays the person's override, got %+v", serve.PendingChooser)
		}
	})
	t.Run("deep", func(t *testing.T) {
		world, session := newDeepWorld(t, "auto-owner", sdd.VerifierPolicy{}, proctest.WithAutonomousDialogue(), clientActor("auto-owner"))
		serve := session.Start(t, "capture", nil)
		session.Gather(t, serve.Instance)
		serve = session.Report(t, serve.Instance, captureDraft())
		proctest.RequireStep(t, serve, "deepDispatch")
		x := dispatchDeep(t, world, session, serve.Instance)
		x.judge(t, ownerPoint())
		proctest.RequireStep(t, x.pickUp(t), "deepAnswer")
		session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
		serve = session.Answer(t, x.instance, "deepProposal", "accept", nil, "")
		proctest.RequireStep(t, serve, "deepUser")
		if !strings.Contains(serve.Instructions, "Answer **revise** with your reason") {
			t.Errorf("in auto mode the agent answers the open point itself:\n%s", serve.Instructions)
		}
	})
}
