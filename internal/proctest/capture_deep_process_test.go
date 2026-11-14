package proctest_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// launchedWait drives a capture to the wait for a round sdd launched, and
// returns the capture instance and the round's child session.
func launchedWait(t *testing.T, world *proctest.World, session *proctest.Session) (string, sdd.SessionID) {
	t.Helper()
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	proctest.RequireStep(t, session.Answer(t, instance, "deepLaunch", "started", nil, ""), "deepWait")
	started := world.Rounds.Started()
	if len(started) != 1 || started[0].Number != 1 {
		t.Fatalf("the dispatch should start the first round's process, got %+v", started)
	}
	return instance, started[0].Session
}

// A round runs in a process of its own: it records its outcome after the
// server that launched it is gone, and a new server picks the outcome up.
func TestCaptureDeep_ARoundOutlivesTheServerThatLaunchedIt(t *testing.T) {
	launcher := headlessLauncher(t)
	world, session := newDeepWorld(t, "deep-outlives", sdd.VerifierPolicy{Runner: "codex"}, proctest.WithDelegateLauncher(launcher))
	instance, _ := launchedWait(t, world, session)
	round := launcher.NextRound(t)

	world.Restart(t)
	driveRound(t, world, round)

	session.Refresh(t)
	proctest.RequireStep(t, session.Report(t, instance, nil), "verifySummary")
}

// A round whose process died mid-round fails the capture's next call at the
// wait, naming that the process ended without an outcome.
func TestCaptureDeep_ARoundWhoseProcessDiedFailsTheWait(t *testing.T) {
	launcher := headlessLauncher(t)
	world, session := newDeepWorld(t, "deep-process-died", sdd.VerifierPolicy{Runner: "codex"}, proctest.WithDelegateLauncher(launcher))
	instance, child := launchedWait(t, world, session)
	launcher.NextRound(t)

	world.Rounds.Kill(t, child, 1)
	session.Refresh(t)
	serve := session.Report(t, instance, nil)

	proctest.RequireStep(t, serve, "deepFailed")
	if !strings.Contains(serve.Instructions, "round 1 (claude) failed: the round's process ended without recording an outcome") {
		t.Fatalf("deepFailed should name the round's process that ended without an outcome:\n%s", serve.Instructions)
	}
}

// A round whose process died leaves the agent CLI it started running in its
// process group: failing the wait stops that group through the agent CLI the
// round recorded.
func TestCaptureDeep_ARoundWhoseProcessDiedStopsItsAgentCLI(t *testing.T) {
	launcher := headlessLauncher(t)
	world, session := newDeepWorld(t, "deep-died-agent", sdd.VerifierPolicy{Runner: "codex"}, proctest.WithDelegateLauncher(launcher))
	instance, child := launchedWait(t, world, session)
	agent := sdd.RoundProcess{PID: 7001, Started: "agent-started"}
	launcher.NextRound(t).AgentStarts(t, agent)

	world.Rounds.Kill(t, child, 1)
	session.Refresh(t)

	proctest.RequireStep(t, session.Report(t, instance, nil), "deepFailed")
	if got := world.Rounds.GroupStoppedThrough(child, 1); got != agent {
		t.Fatalf("the round's process group should be stopped through its agent CLI, got %+v", got)
	}
}

// Abandoning the capture of a round whose process died stops the agent CLI
// that round left running.
func TestCaptureDeep_AbandoningTheCaptureStopsTheAgentCLIOfADeadRound(t *testing.T) {
	launcher := headlessLauncher(t)
	world, session := newDeepWorld(t, "deep-abandon-agent", sdd.VerifierPolicy{Runner: "codex"}, proctest.WithDelegateLauncher(launcher))
	_, child := launchedWait(t, world, session)
	agent := sdd.RoundProcess{PID: 7002, Started: "agent-started"}
	launcher.NextRound(t).AgentStarts(t, agent)
	world.Rounds.Kill(t, child, 1)

	if _, err := world.App.AbandonWorkflowSession(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: session.ID}, "the user stopped"); err != nil {
		t.Fatal(err)
	}

	if got := world.Rounds.GroupStoppedThrough(child, 1); got != agent {
		t.Fatalf("abandoning should stop the dead round's process group through its agent CLI, got %+v", got)
	}
}

// stepClock is a clock the test moves on.
type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stepClock) advance(by time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(by)
}

// A round whose process still runs but recorded no outcome past its timeout
// and the margin fails the wait naming the timeout, and its process is stopped.
func TestCaptureDeep_ARoundWithoutAnOutcomePastItsTimeoutFailsTheWait(t *testing.T) {
	clock := &stepClock{now: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)}
	launcher := headlessLauncher(t)
	world, session := newDeepWorld(t, "deep-past-timeout", sdd.VerifierPolicy{Runner: "codex", RoundTimeout: time.Minute},
		proctest.WithDelegateLauncher(launcher), proctest.WithClock(clock))
	world.Rounds.Idle = true
	instance, child := launchedWait(t, world, session)

	clock.advance(time.Minute + model.RoundOutcomeMargin)
	session.Refresh(t)
	proctest.RequireStep(t, session.Report(t, instance, nil), "deepWait")
	clock.advance(time.Second)
	session.Refresh(t)
	serve := session.Report(t, instance, nil)

	proctest.RequireStep(t, serve, "deepFailed")
	if !strings.Contains(serve.Instructions, "round 1 (claude) failed: the round ran past verify.round_timeout (1m0s) without recording an outcome") {
		t.Fatalf("deepFailed should name the timeout the round ran past:\n%s", serve.Instructions)
	}
	if !world.Rounds.Stopped(child, 1) {
		t.Fatal("the round's process should be stopped once it ran past its timeout")
	}
}

// A round whose server ended after recording the launch, before the round's
// process ran, fails the wait once the margin past its launch passed, naming
// that its process never started.
func TestCaptureDeep_ARoundWhoseProcessNeverStartedFailsTheWait(t *testing.T) {
	clock := &stepClock{now: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)}
	launcher := headlessLauncher(t)
	world, session := newDeepWorld(t, "deep-never-started", sdd.VerifierPolicy{Runner: "codex"},
		proctest.WithDelegateLauncher(launcher), proctest.WithClock(clock))
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	world.Rounds.BeforeStart = func() error {
		world.Restart(t)
		return errors.New("the server ended")
	}
	if _, err := session.AnswerErr(t, instance, "deepDispatch", "delegate", nil, ""); err == nil {
		t.Fatal("the call whose server ended should not answer")
	}
	world.Rounds.BeforeStart = nil

	session.Refresh(t)
	proctest.RequireStep(t, session.Answer(t, instance, "deepLaunch", "started", nil, ""), "deepWait")
	clock.advance(model.RoundOutcomeMargin)
	session.Refresh(t)
	proctest.RequireStep(t, session.Report(t, instance, nil), "deepWait")
	clock.advance(time.Second)
	session.Refresh(t)
	serve = session.Report(t, instance, nil)

	proctest.RequireStep(t, serve, "deepFailed")
	if !strings.Contains(serve.Instructions, "round 1 (claude) failed: the round's process never started") {
		t.Fatalf("deepFailed should name the round's process that never started:\n%s", serve.Instructions)
	}
}

// Abandoning the capture stops the process of its running round.
func TestCaptureDeep_AbandoningTheCaptureStopsItsRound(t *testing.T) {
	launcher := headlessLauncher(t)
	world, session := newDeepWorld(t, "deep-abandon-stops", sdd.VerifierPolicy{Runner: "codex"}, proctest.WithDelegateLauncher(launcher))
	_, child := launchedWait(t, world, session)
	launcher.NextRound(t)

	if _, err := world.App.AbandonWorkflowSession(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: session.ID}, "the user stopped"); err != nil {
		t.Fatal(err)
	}

	if !world.Rounds.Stopped(child, 1) {
		t.Fatal("abandoning the capture should stop its round's process")
	}
}

// Ending the verifier's child session stops the process of its running round.
func TestCaptureDeep_EndingTheVerifierSessionStopsItsRound(t *testing.T) {
	launcher := headlessLauncher(t)
	world, session := newDeepWorld(t, "deep-child-end-stops", sdd.VerifierPolicy{Runner: "codex"}, proctest.WithDelegateLauncher(launcher))
	_, child := launchedWait(t, world, session)
	launcher.NextRound(t)

	if _, err := world.App.AbandonWorkflowSession(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: child}, "the verifier stopped"); err != nil {
		t.Fatal(err)
	}

	if !world.Rounds.Stopped(child, 1) {
		t.Fatal("ending the child session should stop its round's process")
	}
}
