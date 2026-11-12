// The capture's steps that only wait, launch, weigh findings or confirm the
// summary carry the info block alone; the steps that draft or talk to the
// user carry the whole framing (20261112-075604-d-tac-t4r).
package proctest_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

func framingNames(t *testing.T, session *proctest.Session, serve *sdd.WorkflowServe) []string {
	t.Helper()
	blocks, err := session.WF.FramingBlocksFor(t.Context(), session.World.Identity, serve.Framing)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(blocks))
	for _, block := range blocks {
		names = append(names, block.Name)
	}
	return names
}

func TestCaptureFramesItsMechanicalStepsWithInfoAlone(t *testing.T) {
	world, session := newDeepWorld(t, "deep-framing", sdd.VerifierPolicy{})
	serve := session.Start(t, "capture", nil)
	proctest.RequireStep(t, serve, "assemble")
	if names := framingNames(t, session, serve); !slices.Contains(names, "principles") || !slices.Contains(names, "recentMovement") {
		t.Fatalf("assemble should carry the whole framing, got %v", names)
	}
	instance := serve.Instance
	session.Gather(t, instance)
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	if names := framingNames(t, session, serve); !slices.Contains(names, "principles") {
		t.Fatalf("playback should carry the whole framing, got %v", names)
	}
	serve = session.Answer(t, instance, "playback", "confirm", nil, "write it")
	proctest.RequireStep(t, serve, "deepDispatch")
	if names := framingNames(t, session, serve); !slices.Equal(names, []string{"info"}) {
		t.Fatalf("deepDispatch should carry the info block alone, got %v", names)
	}
	x := dispatchDeep(t, world, session, instance)
	x.judge(t, []any{})
	serve = x.pickUp(t)
	proctest.RequireStep(t, serve, "verifySummary")
	if names := framingNames(t, session, serve); !slices.Equal(names, []string{"info"}) {
		t.Fatalf("verifySummary should carry the info block alone, got %v", names)
	}
}
