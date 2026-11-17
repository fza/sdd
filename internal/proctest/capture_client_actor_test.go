package proctest_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// In auto mode sdd writes the actor of the starting MCP client as the
// participant, never the client's raw name: a capture from a client no actor
// names is held until the agent captures that actor.

const unknownClient = "cursor-vscode"

const unknownClientFix = `capture an actor signal for the MCP client "cursor-vscode" first`

func clientActorDraft() map[string]any {
	return map[string]any{
		"body":        "Cursor, the coding agent this project runs through the cursor-vscode MCP client.",
		"entryKind":   "actor",
		"layer":       "process",
		"canonical":   "Cursor",
		"aliases":     []any{unknownClient},
		"actorKind":   "machine",
		"confidence":  "high",
		"widenReport": "searched the graph for an actor naming the cursor-vscode client; none exists",
	}
}

func unknownClientWorld(t *testing.T, options ...proctest.Option) (*proctest.World, *proctest.Session) {
	t.Helper()
	options = append(options, proctest.WithParticipant("Ada"), proctest.WithEntries(authorshipEntries()...))
	world, _ := newCaptureWorld(t, "unused", options...)
	session := world.Open(t, unknownClient)
	session.LogRead(t, "show", []string{captureRefID}, nil)
	return world, session
}

func TestClientActor_AnUnknownClientHoldsTheCaptureUntilItsActorIsCaptured(t *testing.T) {
	world, session := unknownClientWorld(t, proctest.WithAutonomousDialogue())
	held := session.Start(t, "capture", nil).Instance
	serve := session.Report(t, held, captureDraft())
	proctest.RequireStep(t, serve, "assemble")
	if !hasDiagnostic(serve, unknownClientFix) {
		t.Fatalf("the capture should be held naming the fix, got %v", serve.Diagnostics)
	}
	if guide, preflight := world.LLM.Calls("writing-guide"), world.LLM.Calls("preflight"); guide+preflight != 0 {
		t.Errorf("a held capture should spend no model call, got %d guide and %d pre-flight calls", guide, preflight)
	}

	actor := session.Start(t, "capture", nil).Instance
	serve = session.Report(t, actor, clientActorDraft())
	proctest.RequireStep(t, serve, "playback")
	entry := writeCapture(t, world, session, actor, func() *sdd.WorkflowServe {
		return session.Decide(t, actor, "playback", "confirm", nil, "the actor holds")
	})
	if !slices.Equal(entry.Participants, []string{"Cursor"}) {
		t.Errorf("the client's actor signal should name itself as participant, got %v", entry.Participants)
	}

	serve = session.Report(t, held, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	entry = writeCapture(t, world, session, held, func() *sdd.WorkflowServe {
		return session.Decide(t, held, "playback", "confirm", nil, "the draft holds")
	})
	if !slices.Equal(entry.Participants, []string{"Cursor"}) {
		t.Errorf("the held capture should write the actor captured since, got %v", entry.Participants)
	}
}

// An actor signal for anyone but the client would still be written without a
// participant the graph knows.
func TestClientActor_AnotherActorIsHeldToo(t *testing.T) {
	_, session := unknownClientWorld(t, proctest.WithAutonomousDialogue())
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, actorDraft())
	proctest.RequireStep(t, serve, "assemble")
	if !hasDiagnostic(serve, unknownClientFix) {
		t.Errorf("an actor naming another client should be held, got %v", serve.Diagnostics)
	}
}

// A session turned autonomous after its draft passed assemble meets the rule
// at pre-flight, before its model judges the draft, and its revision is held
// at assemble.
func TestClientActor_PreflightRefusesAnUnknownClient(t *testing.T) {
	world, session := unknownClientWorld(t)
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	world.SetDialogue("auto")
	session.Refresh(t)
	serve = session.Decide(t, instance, "playback", "confirm", nil, "the draft holds")
	proctest.RequireStep(t, serve, "reviseOrOverride")
	if !strings.Contains(serve.Instructions, unknownClientFix) {
		t.Fatalf("pre-flight should block the write naming the fix, got:\n%s", serve.Instructions)
	}
	if got := world.LLM.Calls("preflight"); got != 0 {
		t.Errorf("the refusal should spend no pre-flight call, got %d", got)
	}
	serve = session.Decide(t, instance, "reviseOrOverride", "revise", nil, "the client needs its actor")
	proctest.RequireStep(t, serve, "assemble")
	if !hasDiagnostic(serve, unknownClientFix) {
		t.Errorf("the revision should be held naming the fix, got %v", serve.Diagnostics)
	}
}

// Assisted and guided captures are the person's: no client actor is written,
// so an unknown client holds nothing.
func TestClientActor_APersonsCaptureIgnoresTheClient(t *testing.T) {
	cases := map[string][]proctest.Option{"guided": nil, "assisted": {proctest.WithAssistedDialogue()}}
	for name, options := range cases {
		t.Run(name, func(t *testing.T) {
			world, session := unknownClientWorld(t, options...)
			serve := session.Start(t, "capture", nil)
			instance := serve.Instance
			serve = session.Report(t, instance, captureDraft())
			proctest.RequireStep(t, serve, "playback")
			entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
				return session.Answer(t, instance, "playback", "confirm", nil, "capture it")
			})
			if !slices.Equal(entry.Participants, []string{"Ada"}) {
				t.Errorf("the person's capture should write the person, got %v", entry.Participants)
			}
		})
	}
}
