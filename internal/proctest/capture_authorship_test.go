package proctest_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// In auto mode sdd marks what it writes as the agent's: decided-by agent, the
// agent's own reason on a decision, the client's actor as participant and the
// local participant as run-by. A draft past the agent limit parks for a person.

const (
	claudeActorID   = "20260101-090000-s-prc-cla"
	personEntryID   = "20260101-090100-d-tac-per"
	agentEntryID    = "20260101-090200-d-tac-agt"
	authorshipBody  = "Pending directive: the fixture retries the flaky step once."
	authorshipLimit = "agent limit: a conceptual decision takes a person"
)

func authorshipEntries() []*model.Entry {
	return []*model.Entry{
		{ID: claudeActorID, Type: model.TypeSignal, Kind: model.KindActor, Layer: model.LayerProcess, Canonical: "Claude", Aliases: []string{"Claude Code"}, Content: "Claude, the agent."},
		{ID: personEntryID, Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Intent: model.IntentPending, Content: "A person's directive."},
		{ID: agentEntryID, Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Intent: model.IntentPending, DecidedBy: model.DecidedByAgent, Reason: "It unblocks the build.", Content: "An agent's directive."},
	}
}

func authorshipWorld(t *testing.T, options ...proctest.Option) (*proctest.World, *proctest.Session) {
	t.Helper()
	options = append(options, proctest.WithParticipant("Ada"), proctest.WithEntries(authorshipEntries()...))
	world, _ := newCaptureWorld(t, "unused", options...)
	session := world.Open(t, "claude-code")
	session.LogRead(t, "show", []string{captureRefID, personEntryID, agentEntryID}, nil)
	return world, session
}

func directiveDraft(layer string) map[string]any {
	draft := captureDraft()
	draft["body"] = authorshipBody
	draft["entryKind"] = "directive"
	draft["intent"] = "pending"
	draft["layer"] = layer
	return draft
}

// writeCapture confirms the playback and the summary, returning the entry.
func writeCapture(t *testing.T, world *proctest.World, session *proctest.Session, instance string, confirm func() *sdd.WorkflowServe) *model.Entry {
	t.Helper()
	serve := confirm()
	proctest.RequireStep(t, serve, "verifySummary")
	serve = session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches the body"}, "")
	proctest.RequireStatus(t, serve, "completed")
	id, _ := serve.Produced["entryId"].(string)
	return proctest.LoadEntry(t, world.GraphDir, id)
}

func TestAuthorship_AnAutonomousSignalCarriesTheAgentMark(t *testing.T) {
	world, session := authorshipWorld(t, proctest.WithAutonomousDialogue())
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
		return session.Decide(t, instance, "playback", "confirm", nil, "the draft holds")
	})
	if !entry.AgentDecided() || entry.RunBy != "Ada" || !slices.Equal(entry.Participants, []string{"Claude"}) || entry.Reason != "" {
		t.Errorf("an autonomous signal should be decided-by agent, run by the local participant, with the client's actor as participant, got decided-by %q run-by %q participants %v reason %q",
			entry.DecidedBy, entry.RunBy, entry.Participants, entry.Reason)
	}
}

// The participant is the client that started the capture: the CLI joining the
// session later to confirm it does not take its place.
func TestAuthorship_TheStartingClientStaysTheParticipant(t *testing.T) {
	world, session := authorshipWorld(t, proctest.WithAutonomousDialogue())
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")

	cli, _ := world.Resume(t, session.ID, "sdd-cli")
	entry := writeCapture(t, world, cli, instance, func() *sdd.WorkflowServe {
		return cli.Decide(t, instance, "playback", "confirm", nil, "the draft holds")
	})
	if !slices.Equal(entry.Participants, []string{"Claude"}) {
		t.Errorf("the participant should be the actor of the client that started the capture, got %v", entry.Participants)
	}
}

// A client that sends no name has no actor to write for: its autonomous
// capture is refused at the start, and at the write when the session turned
// autonomous after it started.
func TestAuthorship_ANamelessClientCannotCaptureUnattended(t *testing.T) {
	t.Run("at the start", func(t *testing.T) {
		world, _ := authorshipWorld(t, proctest.WithAutonomousDialogue())
		session := world.Open(t, "")
		if _, err := session.StartErr(t, "capture", nil); err == nil || !strings.Contains(err.Error(), "send a name") {
			t.Errorf("the start should be refused telling the client to send a name, got %v", err)
		}
	})
	t.Run("at the write", func(t *testing.T) {
		world, _ := authorshipWorld(t)
		session := world.Open(t, "")
		session.LogRead(t, "show", []string{captureRefID}, nil)
		serve := session.Start(t, "capture", nil)
		instance := serve.Instance
		serve = session.Report(t, instance, captureDraft())
		proctest.RequireStep(t, serve, "playback")
		world.SetDialogue("auto")
		session.Refresh(t)
		if _, err := session.DecideErr(t, instance, "playback", "confirm", nil, "the draft holds"); err == nil || !strings.Contains(err.Error(), "send a name") {
			t.Errorf("the write should be refused telling the client to send a name, got %v", err)
		}
	})
}

func TestAuthorship_AGuidedCaptureCarriesNoMark(t *testing.T) {
	world, session := authorshipWorld(t)
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
		return session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	})
	if entry.AgentDecided() || entry.RunBy != "" || !slices.Equal(entry.Participants, []string{"Ada"}) {
		t.Errorf("a guided capture should be the person's, got decided-by %q run-by %q participants %v", entry.DecidedBy, entry.RunBy, entry.Participants)
	}
}

func TestAuthorship_AnAutonomousDecisionMayStateItsOwnReason(t *testing.T) {
	world, session := authorshipWorld(t, proctest.WithAutonomousDialogue())
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	draft := directiveDraft("tactical")
	draft["reason"] = "The flaky step fails one run in ten."
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
	entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
		return session.Decide(t, instance, "playback", "confirm", nil, "the draft holds")
	})
	if !entry.AgentDecided() || entry.Reason != "The flaky step fails one run in ten." {
		t.Errorf("the decision should carry the agent mark and its reason, got decided-by %q reason %q", entry.DecidedBy, entry.Reason)
	}
}

func TestAuthorship_AnAutonomousDraftReportsNoParticipants(t *testing.T) {
	_, session := authorshipWorld(t, proctest.WithAutonomousDialogue())
	serve := session.Start(t, "capture", nil)
	draft := captureDraft()
	draft["participants"] = []any{"Ada"}
	serve = session.Report(t, serve.Instance, draft)
	proctest.RequireStep(t, serve, "assemble")
	if !hasDiagnostic(serve, "participants are not reported in auto mode") {
		t.Errorf("reported participants should hold an autonomous draft at assemble, got %v", serve.Diagnostics)
	}
}

func TestAuthorship_TheAgentLimitParksTheCapture(t *testing.T) {
	cases := []struct {
		name  string
		draft func() map[string]any
		rule  string
	}{
		{name: "a conceptual directive", draft: func() map[string]any {
			d := directiveDraft("conceptual")
			d["reason"] = "It reads cleaner."
			return d
		}, rule: authorshipLimit},
		{name: "superseding a person's entry", draft: func() map[string]any {
			d := directiveDraft("tactical")
			d["reason"] = "It replaces the old retry."
			d["supersedes"] = []any{personEntryID}
			return d
		}, rule: "agent limit: superseding d-tac-per, which a person decided"},
		{name: "an approval", draft: func() map[string]any {
			d := directiveDraft("tactical")
			d["intent"] = "settled"
			d["reason"] = "I checked it."
			d["approves"] = []any{agentEntryID}
			return d
		}, rule: "agent limit: an approval takes a person"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			world, session := authorshipWorld(t, proctest.WithAutonomousDialogue())
			serve := session.Start(t, "capture", nil)
			instance := serve.Instance
			serve = session.Report(t, instance, tc.draft())
			proctest.RequireStep(t, serve, "agentLimit")
			proctest.RequireStatus(t, serve, "running")
			if serve.Parked == nil || !strings.HasPrefix(serve.Parked.Reason, tc.rule) {
				t.Fatalf("the draft should park for a person naming the rule %q, got %+v", tc.rule, serve.Parked)
			}
			if got := world.LLM.Calls("preflight"); got != 0 {
				t.Errorf("a parked draft should spend no pre-flight call, got %d", got)
			}
			if _, err := session.DecideErr(t, instance, "agentLimit", "adjust", map[string]any{"layer": "tactical"}, "I lower it"); err == nil || !strings.Contains(err.Error(), "agentLimit is parked") {
				t.Errorf("the agent should not answer the parked step, got %v", err)
			}
		})
	}
}

func TestAuthorship_AParkedDraftResumesForAPersonAsTheirs(t *testing.T) {
	world, session := authorshipWorld(t, proctest.WithAutonomousDialogue())
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	draft := directiveDraft("conceptual")
	draft["reason"] = "It reads cleaner."
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "agentLimit")

	world.SetDialogue("guided")
	resumed, serves, err := world.App.ResumeWorkflow(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: session.ID, ClientName: "person"})
	if err != nil {
		t.Fatal(err)
	}
	session.WF = resumed
	serve = servedInstance(serves.Open, instance)
	if serve == nil || serve.Step != "agentLimit" || serve.Parked != nil || serve.PendingChooser == nil {
		t.Fatalf("a guided resume should put the parked draft to the person, got %+v", serve)
	}
	serve = session.Answer(t, instance, "agentLimit", "adjust", map[string]any{"reasonWords": "the old wording confused the team", "intentWords": "do it next"}, "take it on: the old wording confused the team, do it next")
	proctest.RequireStep(t, serve, "playback")
	entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
		return session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	})
	if entry.AgentDecided() || entry.Layer != model.LayerConceptual {
		t.Errorf("the person's capture should be theirs, without the agent's mark, got decided-by %q layer %s", entry.DecidedBy, entry.Layer)
	}
	if entry.Reason != "the old wording confused the team" || entry.ReasonBy != "Ada" || entry.IntentBy != "Ada" {
		t.Errorf("the person's reason should replace the agent's, marked theirs, got %q by %q, intent-by %q", entry.Reason, entry.ReasonBy, entry.IntentBy)
	}
}

// A decision drafted in auto mode and confirmed or written in the guided mode
// is the person's, the drafted reason and intent with it, without asking them
// for words.
func TestAuthorship_AGuidedWriteOfAnAutoDraftedDecisionIsThePersons(t *testing.T) {
	cases := []struct {
		name string
		// toGuided turns the session guided and returns the serve that
		// follows the person's first answer.
		toGuided func(t *testing.T, world *proctest.World, session *proctest.Session, instance string) *sdd.WorkflowServe
	}{
		{name: "a playback confirmed after a mode switch", toGuided: func(t *testing.T, world *proctest.World, session *proctest.Session, instance string) *sdd.WorkflowServe {
			world.SetDialogue("guided")
			session.Refresh(t)
			return session.Answer(t, instance, "playback", "confirm", nil, "capture it")
		}},
		{name: "a parked write the person retries", toGuided: func(t *testing.T, world *proctest.World, session *proctest.Session, instance string) *sdd.WorkflowServe {
			world.LLM.PreflightErr = preflightTimedOut()
			serve := session.Decide(t, instance, "playback", "confirm", nil, "the draft holds")
			proctest.RequireStep(t, serve, "preflightTimedOut")
			serve = session.Decide(t, instance, "preflightTimedOut", "retry", nil, "a slow call may pass")
			if serve.Parked == nil {
				t.Fatalf("a second timeout should park the capture, got step %s", serve.Step)
			}
			world.SetDialogue("guided")
			world.LLM.PreflightErr = nil
			session.Refresh(t)
			// The agent's confirm is no person's: the draft is played back
			// to the person before it is written as theirs.
			serve = session.Answer(t, instance, "preflightTimedOut", "retry", nil, "retry it")
			proctest.RequireStep(t, serve, "playback")
			if serve.PendingChooser == nil {
				t.Fatalf("the playback should wait for the person, got %+v", serve)
			}
			return session.Answer(t, instance, "playback", "confirm", nil, "capture it")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			world, session := authorshipWorld(t, proctest.WithAutonomousDialogue())
			serve := session.Start(t, "capture", nil)
			instance := serve.Instance
			draft := directiveDraft("tactical")
			draft["reason"] = "The flaky step fails one run in ten."
			serve = session.Report(t, instance, draft)
			proctest.RequireStep(t, serve, "playback")

			entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
				return tc.toGuided(t, world, session, instance)
			})
			if entry.AgentDecided() || !slices.Equal(entry.Participants, []string{"Ada"}) {
				t.Errorf("the decision should be the person's, got decided-by %q participants %v", entry.DecidedBy, entry.Participants)
			}
			if entry.Reason != "The flaky step fails one run in ten." || entry.ReasonBy != "Ada" || entry.IntentBy != "Ada" {
				t.Errorf("the drafted reason and intent should be the person's, got %q by %q, intent-by %q", entry.Reason, entry.ReasonBy, entry.IntentBy)
			}
		})
	}
}

func TestAuthorship_APersonApprovesAnAgentsEntry(t *testing.T) {
	world, session := authorshipWorld(t)
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	draft := directiveDraft("tactical")
	draft["intent"] = "settled"
	draft["body"] = "Approval: the agent's retry directive holds."
	draft["approves"] = []any{agentEntryID}
	draft["reasonWords"] = "I read the retry logs"
	draft["intentWords"] = "settled now"
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
	entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
		return session.Answer(t, instance, "playback", "confirm", nil, "I read the retry logs, settled now, capture it")
	})
	if !slices.Equal(entry.Approves, []string{agentEntryID}) {
		t.Fatalf("the approval should list the approved entry, got %v", entry.Approves)
	}
	graph := model.NewGraph([]*model.Entry{proctest.LoadEntry(t, world.GraphDir, agentEntryID), entry})
	if mark := graph.AgentMarks()[agentEntryID]; mark.Marked {
		t.Errorf("an approved entry should lose its mark, got %+v", mark)
	}
}

// A session turned autonomous after its draft passed assemble meets the agent
// limit once the agent confirms it: the draft parks there, nothing is written.
func TestAuthorship_TheWriteGateHoldsTheAgentLimit(t *testing.T) {
	world, session := authorshipWorld(t)
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	draft := directiveDraft("conceptual")
	draft["reasonWords"] = "the old wording confused the team"
	draft["intentWords"] = "do it next"
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
	world.SetDialogue("auto")
	session.Refresh(t)
	serve = session.Decide(t, instance, "playback", "confirm", nil, "the draft holds")
	proctest.RequireStep(t, serve, "agentLimit")
	if serve.Parked == nil || !strings.HasPrefix(serve.Parked.Reason, authorshipLimit) {
		t.Errorf("the draft should park naming the agent limit, got %+v", serve.Parked)
	}
	if got := world.LLM.Calls("preflight"); got != 0 {
		t.Errorf("the parked draft should spend no pre-flight call, got %d", got)
	}
}

// The catch-up lists what an agent decided alone that no person reviewed, and
// drops it once a person approved it.
func TestAuthorship_TheCatchUpListsUnreviewedAgentDecisions(t *testing.T) {
	world, session := authorshipWorld(t)
	serve := session.Start(t, "catch-up", nil)
	if !strings.Contains(serve.Instructions, "## Agent-decided, awaiting review (1)") || !strings.Contains(serve.Instructions, agentEntryID) {
		t.Fatalf("the catch-up should list the agent's unreviewed directive, got:\n%s", serve.Instructions)
	}
	session.Report(t, serve.Instance, map[string]any{"briefing": "A briefing."})
	session.Answer(t, serve.Instance, "junction", "conclude", nil, "nothing for now")

	capture := session.Start(t, "capture", nil)
	draft := directiveDraft("tactical")
	draft["intent"] = "settled"
	draft["body"] = "Approval: the agent's retry directive holds."
	draft["approves"] = []any{agentEntryID}
	draft["reasonWords"] = "I read the retry logs"
	draft["intentWords"] = "settled now"
	serve = session.Report(t, capture.Instance, draft)
	proctest.RequireStep(t, serve, "playback")
	writeCapture(t, world, session, capture.Instance, func() *sdd.WorkflowServe {
		return session.Answer(t, capture.Instance, "playback", "confirm", nil, "I read the retry logs, settled now, capture it")
	})

	serve = session.Start(t, "catch-up", nil)
	if strings.Contains(serve.Instructions, "## Agent-decided, awaiting review") {
		t.Errorf("an approved decision should leave the catch-up lane, got:\n%s", serve.Instructions)
	}
}
