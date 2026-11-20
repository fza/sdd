// A revision delivered to a waiting headless verifier launches its round in
// the call that delivered it, whichever call that is; a launch that call
// owed and never made is launched by the move's next call.
package proctest_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
	"github.com/networkteam/sdd/test"
)

const relaunchBodyFile = "---\nentryKind: gap\nlayer: tac\n---\nA tactical gap: the fixture observes something.\n"

// sentBackToAssemble drives an assisted capture whose body comes from a
// staged file through a headless verifier's clean first round to the
// person's approval gate, where their adjust sends the draft back to assemble
// to wait for a revision.
func sentBackToAssemble(t *testing.T, connID string, launcher *test.ScriptedLauncher) (*proctest.World, *proctest.Session, string, string, *test.ScriptedRound) {
	t.Helper()
	world, session := newDeepWorld(t, connID, sdd.VerifierPolicy{Runner: "claude"}, proctest.WithDelegateLauncher(launcher), proctest.WithAssistedDialogue(), clientActor(connID))
	handle := session.Stage(t, "draft.md", []byte(relaunchBodyFile))
	instance := session.Start(t, "capture", nil).Instance
	session.Gather(t, instance)
	proctest.RequireStep(t, session.Report(t, instance, stagedBodyDraft(handle)), "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	first := launcher.NextRound(t)
	driveRound(t, world, first)
	session.Refresh(t)
	proctest.RequireStep(t, session.Answer(t, instance, "deepLaunch", "started", nil, ""), "playback")
	proctest.RequireStep(t, session.Answer(t, instance, "playback", "adjust", nil, "say it happens twice"), "assemble")
	return world, session, instance, handle, first
}

// The adjusted body staged as a patch re-advances the capture past
// pre-flight to the live verifier: the staging call that sends it the
// revision launches the round that reads it.
func TestCaptureDeep_ARevisionStagedAsAPatchLaunchesTheHeadlessRound(t *testing.T) {
	launcher := headlessLauncher(t)
	world, session, instance, handle, first := sentBackToAssemble(t, "deep-relaunch-staged", launcher)
	staged, err := session.StageItems(t, sdd.StageItem{Name: handle, Patches: []types.PatchPair{{Old: "observes something.", New: "observes something twice."}}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(staged.Files[0].Bound, instance) {
		t.Fatalf("the patch should reach the capture's body, got %+v", staged.Files)
	}
	if len(staged.Dispatches) != 1 || staged.Dispatches[0].Runner != "claude" || !strings.Contains(staged.Dispatches[0].Instructions, "launch nothing") {
		t.Fatalf("the staging call should owe the dispatch of the round sdd launched, got %+v", staged.Dispatches)
	}
	round := launcher.NextRound(t)
	if round.Launch.Session != first.Launch.Session || round.Round.Number != 2 || round.Round.Resume != "cli-session" {
		t.Fatalf("the staged revision should resume the verifier's session in its child, got round %+v of %s", round.Round, round.Launch.Session)
	}
	session.Refresh(t)
	if serve := session.Report(t, instance, nil); len(serve.Dispatches) != 0 {
		t.Fatalf("a call while the round runs should launch no other, got %+v", serve.Dispatches)
	}
	judgeRevision(t, world, round)
	session.Refresh(t)
	proctest.RequireStep(t, session.Report(t, instance, nil), "playback")
}

// A revision the verifier received whose round the delivering call failed to
// launch stays owed: the move's next call launches it, and says so.
func TestCaptureDeep_AnOwedRoundLaunchesOnTheNextCall(t *testing.T) {
	launcher := headlessLauncher(t)
	world, session, instance, handle, first := sentBackToAssemble(t, "deep-relaunch-owed", launcher)
	launcher.Refuse(errors.New("the launcher is down"))
	_, err := session.StageItems(t, sdd.StageItem{Name: handle, Patches: []types.PatchPair{{Old: "observes something.", New: "observes something twice."}}})
	if err == nil || !strings.Contains(err.Error(), "the launcher is down") {
		t.Fatalf("the staging should fail with the launch it could not make, got %v", err)
	}
	launcher.Refuse(nil)
	session.Refresh(t)
	serve := session.Report(t, instance, nil)
	proctest.RequireStep(t, serve, "deepWait")
	if len(serve.Dispatches) != 1 || serve.Dispatches[0].Runner != "claude" {
		t.Fatalf("the next call should owe the dispatch of the round it launched, got %+v", serve.Dispatches)
	}
	round := launcher.NextRound(t)
	if round.Launch.Session != first.Launch.Session || round.Round.Number != 2 {
		t.Fatalf("the owed round should launch in the verifier's child, got round %+v of %s", round.Round, round.Launch.Session)
	}
	judgeRevision(t, world, round)
	session.Refresh(t)
	serve = session.Report(t, instance, nil)
	proctest.RequireStep(t, serve, "playback")
	if len(serve.Dispatches) != 0 {
		t.Fatalf("a round that ran owes no other, got %+v", serve.Dispatches)
	}
}

// judgeRevision has the resumed verifier pick the revision up and judge it
// clean, and ends its round.
func judgeRevision(t *testing.T, world *proctest.World, round *test.ScriptedRound) {
	t.Helper()
	delegate := world.Delegate(t, &sdd.WorkflowDispatch{Session: round.Launch.Session})
	proctest.RequireStep(t, delegate.Report(t, round.Launch.Instance, nil), "read")
	delegate.Answer(t, round.Launch.Instance, "read", "judge", map[string]any{"findings": []any{}}, "")
	round.Finish("cli-session", nil)
	waitForRounds(t, world, round.Launch.Session, round.Round.Number)
}
