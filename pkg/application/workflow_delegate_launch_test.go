package application_test

import (
	"errors"
	"strings"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/test"
)

// deliverPlan drives a launched round's delegate through the application, the
// way a headless CLI does through its own sdd server.
func (f *delegateFixture) deliverPlan(t *testing.T, round *test.ScriptedRound) {
	t.Helper()
	f.advance(t, round.Launch.Session, round.Launch.Instance, map[string]any{"plan": "f1 from the launched delegate"})
}

// recorded waits until the child session keeps the launcher sessions of its
// ended rounds, and returns them.
func (f *delegateFixture) recorded(t *testing.T, child sdd.SessionID, rounds int) []string {
	t.Helper()
	var kept []string
	test.Eventually(t, "recording the ended rounds", func() bool {
		var err error
		kept, err = f.app.DelegateLaunches(t.Context(), f.identity, child)
		if err != nil {
			t.Fatal(err)
		}
		return len(kept) >= rounds
	})
	return kept
}

func TestLaunchedRoundRunsInTheBackground(t *testing.T) {
	launcher := test.NewScriptedLauncher(t, "scripted")
	f := newDelegateApplication(t, launcher)
	_, dispatch := f.fork(t)
	if dispatch.Runner != "scripted" || !strings.Contains(dispatch.Instructions, "launch nothing") || !strings.Contains(dispatch.Instructions, "works on its own while this move waits") {
		t.Fatalf("a launched round says sdd launched it and it works on its own, got %+v", dispatch)
	}
	if dispatch.Session != "" || dispatch.Instance != "" || dispatch.Prompt != "" || dispatch.ContinuePrompt != "" || dispatch.Agent != "" {
		t.Fatalf("a delegate sdd launched hands the dispatching side neither its handles nor a prompt, got %+v", dispatch)
	}
	round := launcher.NextRound(t)
	held := f.advance(t, f.parent, f.dispatch, nil)
	if held.Step != "wait" {
		t.Fatalf("the dispatching move waits while the round runs, at %s", held.Step)
	}
	f.deliverPlan(t, round)
	if picked := f.advance(t, f.parent, f.dispatch, nil); picked.Step != "answer" {
		t.Fatalf("the round's delivery lands in the dispatching move while the round still runs, at %s", picked.Step)
	}
	round.Finish("cli-1", nil)
	f.recorded(t, round.Launch.Session, 1)
}

func TestLauncherKeepsItsSessionPerChildAndResumesEachRound(t *testing.T) {
	launcher := test.NewScriptedLauncher(t, "scripted")
	f := newDelegateApplication(t, launcher)
	f.fork(t)
	first := launcher.NextRound(t)
	child := first.Launch.Session
	f.deliverPlan(t, first)
	f.advance(t, f.parent, f.dispatch, nil)
	sent := f.advance(t, f.parent, f.dispatch, map[string]any{"chooser": "answer", "choice": "reply", "fields": map[string]any{"brief": "a1"}})
	if len(sent.Dispatches) != 1 || sent.Dispatches[0].Runner != "scripted" || sent.Dispatches[0].Session != "" || sent.Dispatches[0].ContinuePrompt != "" {
		t.Fatalf("sending the next input launches the next round and hands over no handle, got %+v", sent.Dispatches)
	}
	// The first round's agent still runs after it delivered: the next round
	// starts only once it ended, and resumes its session.
	first.Finish("cli-1", nil)
	second := launcher.NextRound(t)
	if first.Round.Number != 1 || first.Round.Resume != "" || second.Round.Number != 2 || second.Round.Resume != "cli-1" {
		t.Fatalf("each round resumes the launcher session of the one before, got %+v then %+v", first.Round, second.Round)
	}
	if len(launcher.Launches()) != 2 {
		t.Fatalf("one launch per round, got %d", len(launcher.Launches()))
	}
	for _, round := range []*test.ScriptedRound{first, second} {
		if round.Launch.Session != child {
			t.Fatalf("every launch names the child session, got %s", round.Launch.Session)
		}
		if strings.Contains(round.Round.Prompt, string(f.parent)) || !strings.Contains(round.Round.Prompt, string(child)) {
			t.Fatalf("a launched prompt carries the child handle and never the dispatching one, got %q", round.Round.Prompt)
		}
	}
	f.advance(t, second.Launch.Session, second.Launch.Instance, nil)
	f.advance(t, second.Launch.Session, second.Launch.Instance, map[string]any{"chooser": "respond", "choice": "again", "fields": map[string]any{"plan": "f2 from the launched delegate"}})
	second.Finish("cli-2", nil)
	if kept := f.recorded(t, child, 2); strings.Join(kept, ",") != "cli-1,cli-2" {
		t.Fatalf("the launcher sessions are kept with the child session, got %v", kept)
	}
}

func TestFailedRoundFailsTheDispatchingMovesNextCall(t *testing.T) {
	launcher := test.NewScriptedLauncher(t, "scripted")
	f := newDelegateApplication(t, launcher)
	f.fork(t)
	round := launcher.NextRound(t)
	round.Finish("", errors.New("agent CLI not found"))
	var failed error
	test.Eventually(t, "the failed round reaching the dispatching move", func() bool {
		_, failed = f.session(t, f.parent).Advance(t.Context(), f.identity, sdd.WorkflowAdvanceRequest{Instance: f.dispatch})
		return failed != nil
	})
	if !strings.Contains(failed.Error(), "agent CLI not found") || !strings.Contains(failed.Error(), "round 1 (scripted) failed") {
		t.Fatalf("the next call should fail with the round's error, got %v", failed)
	}
	test.Eventually(t, "ending the failed round's delegate", func() bool { return f.ended(t, round.Launch.Session) != nil })
	if end := f.ended(t, round.Launch.Session); !strings.Contains(end.Reason, "agent CLI not found") {
		t.Fatalf("a failed round ends its delegate naming the error, got %+v", end)
	}
	if held := f.advance(t, f.parent, f.dispatch, nil); held.Step != "wait" {
		t.Fatalf("the failure surfaces once and its fork ends, at %s", held.Step)
	}
}

func TestLaunchedRoundEndingWithoutADeliveryFails(t *testing.T) {
	launcher := test.NewScriptedLauncher(t, "scripted")
	f := newDelegateApplication(t, launcher)
	f.fork(t)
	round := launcher.NextRound(t)
	round.FinishSaying("cli-1", "The sdd tools need approval, so I stop.")
	var failed error
	test.Eventually(t, "the empty round reaching the dispatching move", func() bool {
		_, failed = f.session(t, f.parent).Advance(t.Context(), f.identity, sdd.WorkflowAdvanceRequest{Instance: f.dispatch})
		return failed != nil
	})
	if !strings.Contains(failed.Error(), "ended without the delegate delivering or concluding") || !strings.Contains(failed.Error(), "The sdd tools need approval, so I stop.") {
		t.Fatalf("a round ending with nothing delivered should fail naming its last message, got %v", failed)
	}
	test.Eventually(t, "ending the empty round's delegate", func() bool { return f.ended(t, round.Launch.Session) != nil })
}

func TestServedBlocksRecordAfterALaunchedRoundDelivered(t *testing.T) {
	launcher := test.NewScriptedLauncher(t, "scripted")
	f := newDelegateApplication(t, launcher)
	workflow := f.session(t, f.parent)
	_, err := workflow.Advance(t.Context(), f.identity, sdd.WorkflowAdvanceRequest{Instance: f.dispatch, Report: delegateAnswer})
	if err != nil {
		t.Fatal(err)
	}
	round := launcher.NextRound(t)
	f.deliverPlan(t, round)
	round.Finish("cli-1", nil)
	f.recorded(t, round.Launch.Session, 1)
	proof := proofOf(t, workflow.ServeText("lane step", servedLane, nil))
	err = workflow.RecordServed(t.Context(), f.identity)
	if err != nil {
		t.Fatalf("the anchors of the dispatching call should land although the delegate wrote the session meanwhile: %v", err)
	}
	reloaded := f.session(t, f.parent)
	err = reloaded.BeginCall([]string{proof}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.ServeText("lane step", servedLane, nil).Held {
		t.Fatal("the anchors should be in the session ledger")
	}
	if picked := f.advance(t, f.parent, f.dispatch, nil); picked.Step != "answer" {
		t.Fatalf("the delegate's delivery should still land in the dispatching move, at %s", picked.Step)
	}
}
