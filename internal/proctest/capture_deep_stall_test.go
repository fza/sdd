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
	return sdd.DelegateRun{Runner: "codex", Round: func(context.Context, sdd.DelegateRound) (sdd.DelegateRoundEnd, error) {
		<-l.release
		return sdd.DelegateRoundEnd{}, nil
	}}, nil
}

// A headless round that runs past verify.round_timeout is stopped and fails,
// so the waiting capture moves to deepFailed naming the limit.
func TestCaptureDeep_AStalledRoundFailsPastItsTimeLimit(t *testing.T) {
	launcher := stalledLauncher{release: make(chan struct{})}
	t.Cleanup(func() { close(launcher.release) })
	world, session := newDeepWorld(t, "deep-stalled", sdd.VerifierPolicy{Runner: "codex", RoundTimeout: 50 * time.Millisecond}, proctest.WithDelegateLauncher(launcher))
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
