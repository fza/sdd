package proctest_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/test"
)

// stalledLauncher launches rounds that never return, not even once their
// context ends, as a headless agent whose script failed while its process
// stayed alive.
type stalledLauncher struct{ release chan struct{} }

func (l stalledLauncher) LaunchDelegate(context.Context, sdd.DelegateLaunch) (sdd.DelegateRun, error) {
	return sdd.DelegateRun{Runner: "claude", Round: func(context.Context, sdd.DelegateRound) (sdd.DelegateRoundEnd, error) {
		<-l.release
		return sdd.DelegateRoundEnd{}, nil
	}}, nil
}

// A headless round that runs past verify.round_timeout is stopped and fails,
// so the waiting capture moves to deepFailed naming the limit.
func TestCaptureDeep_AStalledRoundFailsPastItsTimeLimit(t *testing.T) {
	launcher := stalledLauncher{release: make(chan struct{})}
	t.Cleanup(func() { close(launcher.release) })
	world, session := newDeepWorld(t, "deep-stalled", sdd.VerifierPolicy{Runner: "claude", RoundTimeout: 50 * time.Millisecond}, proctest.WithDelegateLauncher(launcher))
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	serve = session.Answer(t, instance, "deepLaunch", "started", nil, "")
	proctest.RequireStep(t, serve, "deepWait")
	test.Eventually(t, "the stalled round failing the exchange", func() bool {
		workflow, err := world.App.RefreshWorkflow(t.Context(), world.Identity, session.ID)
		if err != nil {
			t.Fatal(err)
		}
		serve, err = workflow.Advance(t.Context(), world.Identity, sdd.WorkflowAdvanceRequest{Instance: instance})
		if err != nil {
			t.Fatal(err)
		}
		return serve.Step != "deepWait"
	})
	proctest.RequireStep(t, serve, "deepFailed")
	if text := test.Untagged(serve.Instructions); !strings.Contains(text, "ran past verify.round_timeout (50ms) and was stopped") {
		t.Fatalf("deepFailed should name the time limit the round ran past:\n%s", text)
	}
}

// A subagent that ended without delivering is reported at the wait: the
// capture puts the failure to the user, and the delegate's session ends.
func TestCaptureDeep_ASubagentThatEndedWithoutDeliveringFailsTheWait(t *testing.T) {
	world, session := newDeepWorld(t, "deep-gone", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	serve := session.Answer(t, x.instance, "deepLaunch", "started", nil, "")
	proctest.RequireStep(t, serve, "deepWait")
	if text := strings.Join(serve.Diagnostics, "\n"); !strings.Contains(text, "Delegate ended without delivering? Report deepGone") {
		t.Fatalf("the wait should name the report for a delegate that ended:\n%s", text)
	}
	serve = session.Report(t, x.instance, map[string]any{"deepGone": "the subagent stopped: it could not reach sdd"})
	proctest.RequireStep(t, serve, "deepFailed")
	if text := test.Untagged(serve.Instructions); !strings.Contains(text, "the delegate ended without delivering; it returned: the subagent stopped: it could not reach sdd") {
		t.Fatalf("deepFailed should name the delegate that ended without delivering:\n%s", text)
	}
	if _, _, err := world.App.ResumeWorkflow(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: x.dispatch.Session}); err == nil {
		t.Fatal("the delegate's session should end with the report")
	}
}

// A delegate that makes no call for verify.idle_timeout ends, and the wait
// fails instead of serving another check.
func TestCaptureDeep_ASilentDelegateFailsTheWaitPastTheIdleBound(t *testing.T) {
	clock := &stepClock{now: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)}
	world, session := newDeepWorld(t, "deep-idle", sdd.VerifierPolicy{IdleTimeout: 10 * time.Minute}, proctest.WithClock(clock))
	x := startDeep(t, world, session, captureDraft())
	proctest.RequireStep(t, session.Answer(t, x.instance, "deepLaunch", "started", nil, ""), "deepWait")

	clock.advance(10 * time.Minute)
	session.Refresh(t)
	proctest.RequireStep(t, session.Report(t, x.instance, nil), "deepWait")
	clock.advance(time.Second)
	session.Refresh(t)
	serve := session.Report(t, x.instance, nil)

	proctest.RequireStep(t, serve, "deepFailed")
	if text := test.Untagged(serve.Instructions); !strings.Contains(text, "the verify delegate made no call for 10m0s (verify.idle_timeout)") {
		t.Fatalf("deepFailed should name the idle bound:\n%s", text)
	}
}
