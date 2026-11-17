package proctest_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// In an assisted session the agent drafts and checks a capture alone, as in
// auto, and the person approves the checked draft once at playback. The
// approved entry is the person's: approved-by names them, reason-by and
// intent-by who stated its reason and intent.

const assistedReason = "The flaky step fails one run in ten."

func assistedDirective() map[string]any {
	draft := directiveDraft("conceptual")
	draft["intent"] = "guiding"
	draft["reason"] = assistedReason
	return draft
}

// sealedCapture drives an assisted capture of draft to its approval gate.
func sealedCapture(t *testing.T, session *proctest.Session, draft map[string]any) string {
	t.Helper()
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
	return instance
}

func TestAssisted_TheChecksRunBeforeThePersonSeesTheDraft(t *testing.T) {
	world, session := authorshipWorld(t, proctest.WithAssistedDialogue())
	serve := session.Start(t, "capture", nil)
	if !strings.Contains(serve.Instructions, "In assisted mode you draft and check the entry alone") {
		t.Errorf("the draft should be assembled as the agent's, for the person's approval, got:\n%s", serve.Instructions)
	}
	serve = session.Report(t, serve.Instance, assistedDirective())
	proctest.RequireStep(t, serve, "playback")
	if world.LLM.Calls("writing-guide") != 1 || world.LLM.Calls("preflight") != 1 {
		t.Errorf("the guide and pre-flight should run before the playback, got %d guide and %d pre-flight calls", world.LLM.Calls("writing-guide"), world.LLM.Calls("preflight"))
	}
	if !strings.HasPrefix(serve.Goal, "play the checked draft back to the person") || serve.PendingChooser == nil {
		t.Errorf("the playback should be put to the person, got goal %q", serve.Goal)
	}
	if !strings.Contains(serve.Instructions, "This play-back is the person's one approval of the entry") || strings.Contains(serve.Instructions, "Verify the draft yourself") {
		t.Errorf("the playback should ask the person to approve, got:\n%s", serve.Instructions)
	}
}

// The entry is the person's, so the draft may name its participants, and a
// client that sends no name may capture.
func TestAssisted_TheDraftIsWrittenForThePerson(t *testing.T) {
	world, _ := authorshipWorld(t, proctest.WithAssistedDialogue())
	session := world.Open(t, "")
	session.LogRead(t, "show", []string{captureRefID}, nil)
	draft := captureDraft()
	draft["participants"] = []any{"Ada", "Claude"}
	instance := sealedCapture(t, session, draft)
	entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
		return session.Answer(t, instance, "playback", "confirm", nil, "ok")
	})
	if !slices.Equal(entry.Participants, []string{"Ada", "Claude"}) || entry.ApprovedBy != "Ada" {
		t.Errorf("the approved entry should keep the reported participants, got %v approved-by %q", entry.Participants, entry.ApprovedBy)
	}
}

func TestAssisted_TheAgentCannotApprove(t *testing.T) {
	_, session := authorshipWorld(t, proctest.WithAssistedDialogue())
	instance := sealedCapture(t, session, assistedDirective())
	if _, err := session.DecideErr(t, instance, "playback", "confirm", nil, "the draft holds"); err == nil || !strings.Contains(err.Error(), "the person's approval") {
		t.Fatalf("the agent's own confirm should be refused at the approval gate, got %v", err)
	}
}

// The play-back names a directive's intent plainly and says how to ask the
// person for a reason they did not give, one for each draft or one shared.
func TestAssisted_ThePlaybackAsksForThePersonsReason(t *testing.T) {
	_, session := authorshipWorld(t, proctest.WithAssistedDialogue())
	serve := session.Start(t, "capture", nil)
	first := serve.Instance
	serve = session.Report(t, first, assistedDirective())
	proctest.RequireStep(t, serve, "playback")
	for _, want := range []string{"`guiding`: a standing guideline", "Other: you write the reason", "through the AskUserQuestion tool"} {
		if !strings.Contains(serve.Instructions, want) {
			t.Errorf("the playback should say %q, got:\n%s", want, serve.Instructions)
		}
	}
	if strings.Contains(serve.Instructions, "reason-by: agent") {
		t.Errorf("no approval should leave the agent's reason as the agent's, got:\n%s", serve.Instructions)
	}
	second := assistedDirective()
	second["body"] = strings.Replace(authorshipBody, ".", ", in every suite.", 1)
	sealedCapture(t, session, second)
	serve, err := session.WF.Advance(t.Context(), session.World.Identity, sdd.WorkflowAdvanceRequest{Instance: first})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"One reason for all", "A reason per entry", "it applies to every entry unless you name which"} {
		if !strings.Contains(serve.Instructions, want) {
			t.Errorf("a playback of several drafts should offer %q, got:\n%s", want, serve.Instructions)
		}
	}
}

// Every approved decision carries the person's reason: a bare approval is
// refused until they state one or pick the agent's, which is then theirs, as
// the intent they approved is.
func TestAssisted_AnApprovedDecisionCarriesThePersonsReason(t *testing.T) {
	world, session := authorshipWorld(t, proctest.WithAssistedDialogue())
	instance := sealedCapture(t, session, assistedDirective())
	if _, err := session.AnswerErr(t, instance, "playback", "confirm", nil, "ok"); err == nil || !strings.Contains(err.Error(), "without the person's reason") {
		t.Fatalf("a bare approval of a decision should be refused for the person's reason, got %v", err)
	}
	picked := "ok\nThe drafted reason: " + assistedReason
	entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
		return session.Answer(t, instance, "playback", "confirm", map[string]any{"reasonWords": assistedReason}, picked)
	})
	if entry.ApprovedBy != "Ada" || entry.AgentDecided() || entry.DecidedBy != "" || entry.RunBy != "" {
		t.Errorf("the approved entry should be the person's, got approved-by %q decided-by %q run-by %q", entry.ApprovedBy, entry.DecidedBy, entry.RunBy)
	}
	if entry.Reason != assistedReason || entry.ReasonBy != "Ada" || entry.IntentBy != "Ada" {
		t.Errorf("the picked reason and the approved intent should be the person's, got reason %q by %q, intent by %q", entry.Reason, entry.ReasonBy, entry.IntentBy)
	}
	if !slices.Equal(entry.Participants, []string{"Ada"}) {
		t.Errorf("the approved entry should name the local participant, got %v", entry.Participants)
	}
}

func TestAssisted_ThePersonsOwnReasonIsStoredAsTheirs(t *testing.T) {
	world, session := authorshipWorld(t, proctest.WithAssistedDialogue())
	instance := sealedCapture(t, session, assistedDirective())
	words := map[string]any{"reasonWords": "a red build blocks the whole team", "intentWords": "from now on"}
	entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
		return session.Answer(t, instance, "playback", "confirm", words, "yes: a red build blocks the whole team, so from now on")
	})
	if entry.Reason != "a red build blocks the whole team" || entry.ReasonBy != "Ada" || entry.IntentBy != "Ada" {
		t.Errorf("the person's words should be stored as their reason and intent, got reason %q by %q, intent by %q", entry.Reason, entry.ReasonBy, entry.IntentBy)
	}
}

// The agent limit binds what an agent writes alone; a draft the person
// approves needs no such bound.
func TestAssisted_TheAgentLimitDoesNotHoldAnApprovedDraft(t *testing.T) {
	world, session := authorshipWorld(t, proctest.WithAssistedDialogue())
	draft := assistedDirective()
	draft["intent"] = "settled"
	draft["approves"] = []any{agentEntryID}
	instance := sealedCapture(t, session, draft)
	entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
		return session.Answer(t, instance, "playback", "confirm", map[string]any{"reasonWords": "the agent's entry holds"}, "approved: the agent's entry holds")
	})
	if !slices.Equal(entry.Approves, []string{agentEntryID}) || entry.ApprovedBy != "Ada" {
		t.Errorf("the person's approval should be written, got approves %v approved-by %q", entry.Approves, entry.ApprovedBy)
	}
}

// The catch-up names who approved an entry and who gave its reason.
func TestAssisted_TheCatchUpNamesTheApproval(t *testing.T) {
	approved := &model.Entry{ID: "20260101-090300-d-tac-apr", Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical, Intent: model.IntentPending,
		Participants: []string{"Ada"}, ApprovedBy: "Ada", Reason: assistedReason, ReasonBy: model.DraftedByAgent, IntentBy: model.DraftedByAgent, Content: "An approved directive."}
	_, session := authorshipWorld(t, proctest.WithEntries(approved))
	serve := session.Start(t, "catch-up", nil)
	if !strings.Contains(serve.Instructions, approved.ID+" tactical directive decision [approved-by: Ada] [reason-by: agent]") {
		t.Errorf("the briefing should name the approval, got:\n%s", serve.Instructions)
	}
}

// Sent back, the draft is revised and checked again, and the person sees it
// once more before anything is written.
func TestAssisted_AnAdjustPlaysTheRevisionBackAgain(t *testing.T) {
	world, session := authorshipWorld(t, proctest.WithAssistedDialogue())
	instance := sealedCapture(t, session, assistedDirective())
	serve := session.Answer(t, instance, "playback", "adjust", map[string]any{"body": "Guiding directive: the fixture retries the flaky step once, never twice."}, "say it is never twice")
	proctest.RequireStep(t, serve, "playback")
	if world.LLM.Calls("preflight") != 2 {
		t.Errorf("the revision should be checked again before its playback, got %d pre-flight calls", world.LLM.Calls("preflight"))
	}
	if _, err := session.AnswerErr(t, instance, "playback", "confirm", nil, "say it is never twice"); err == nil {
		t.Error("the words that sent the draft back should not approve the revision")
	}
}

// With no answer the draft waits at the gate, listed as parked, and resumes
// in auto as the agent's or in guided as the person's.
func TestAssisted_AnUnansweredDraftParksAtTheGate(t *testing.T) {
	t.Run("listed", func(t *testing.T) {
		world, session := authorshipWorld(t, proctest.WithAssistedDialogue())
		sealedCapture(t, session, captureDraft())
		world.Restart(t)
		sessions, err := world.App.ListWorkflowSessions(t.Context(), world.Identity, "proctest")
		if err != nil {
			t.Fatal(err)
		}
		if parked := parkedMove(sessions, session.ID); parked == nil || parked.Step != "playback" || !strings.Contains(parked.Reason, "approval") {
			t.Fatalf("the sealed draft should be listed as parked at playback for the person's approval, got %+v", parked)
		}
	})
	t.Run("resumed in auto", func(t *testing.T) {
		world, session := authorshipWorld(t, proctest.WithAssistedDialogue())
		signal := sealedCapture(t, session, captureDraft())
		directive := sealedCapture(t, session, assistedDirective())
		world.SetDialogue("auto")
		resumed, _ := world.Resume(t, session.ID, "claude-code")
		entry := writeCapture(t, world, resumed, signal, func() *sdd.WorkflowServe {
			return resumed.Decide(t, signal, "playback", "confirm", nil, "the draft holds")
		})
		if !entry.AgentDecided() || entry.ApprovedBy != "" {
			t.Errorf("a draft the agent confirms in auto should be the agent's, got decided-by %q approved-by %q", entry.DecidedBy, entry.ApprovedBy)
		}
		serve := resumed.Decide(t, directive, "playback", "confirm", nil, "the draft holds")
		if serve.Step != "agentLimit" || serve.Parked == nil {
			t.Fatalf("a draft past the agent limit should park at it in auto, got step %s parked %+v", serve.Step, serve.Parked)
		}
		world.SetDialogue("guided")
		_, served := world.Resume(t, session.ID, "person")
		serve = servedInstance(served.Open, directive)
		if serve == nil || !strings.Contains(serve.Instructions, "drafted this capture in assisted mode") || strings.Contains(serve.Instructions, "in auto mode") {
			t.Errorf("the parked draft should be named as drafted in assisted mode, got %+v", serve)
		}
	})
	t.Run("resumed in guided", func(t *testing.T) {
		world, session := authorshipWorld(t, proctest.WithAssistedDialogue())
		instance := sealedCapture(t, session, captureDraft())
		world.SetDialogue("guided")
		resumed, _ := world.Resume(t, session.ID, "person")
		entry := writeCapture(t, world, resumed, instance, func() *sdd.WorkflowServe {
			return resumed.Answer(t, instance, "playback", "confirm", nil, "capture it")
		})
		if entry.AgentDecided() || entry.ApprovedBy != "" {
			t.Errorf("a draft the person confirms in guided should be theirs without an approval mark, got decided-by %q approved-by %q", entry.DecidedBy, entry.ApprovedBy)
		}
	})
}
