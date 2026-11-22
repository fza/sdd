package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// No kind owes a reason, in any dialogue mode, and a directive owes no words
// of the user's for its intent; a reason or intent given is stored.

func reasonFreeDrafts() map[string]map[string]any {
	role := roleDraft()
	delete(role, "reasonWords")
	role["refs"] = []any{map[string]any{"id": identityActorID, "kind": "grounded-in", "desc": "the actor the role binds"}}
	focus := focusDraft()
	delete(focus, "reasonWords")
	return map[string]map[string]any{
		"role":  role,
		"focus": focus,
		"procedure": {
			"body":       "The examine move looks at one area and hands back its synthesis.\n\n## unit: examine\n\nExamine the area and report the synthesis.",
			"entryKind":  "procedure",
			"layer":      "process",
			"canonical":  "examine-area",
			"confidence": "high",
			"procedureSpec": map[string]any{
				"state": map[string]any{"synthesis": map[string]any{"type": "text", "desc": "the outcome the run hands back"}},
				"steps": []any{map[string]any{
					"id": "examine", "collect": []any{"synthesis"},
					"transitions": []any{map[string]any{"when": "hasSynthesis", "to": "end(completed)"}},
				}},
			},
			"widenReport": "searched the graph for an existing examine move before drafting",
		},
		"aspiration": {
			"body":        "The fixture project aims to stay small enough for one person to hold in their head.",
			"entryKind":   "aspiration",
			"layer":       "strategic",
			"confidence":  "medium",
			"widenReport": "searched the graph for prior aspirations before drafting",
		},
		"activity":  decisionDraft("activity"),
		"directive": decisionDraft("directive"),
		"plan":      planDraft(),
	}
}

// agentWritable are the reason-free drafts an agent may write alone; the
// agent limit parks the others.
var agentWritable = map[string]bool{"activity": true, "directive": true, "plan": true}

// reasonKindsWorld is authorshipWorld plus the actor a role binds to.
func reasonKindsWorld(t *testing.T, options ...proctest.Option) (*proctest.World, *proctest.Session) {
	t.Helper()
	world, session := authorshipWorld(t, append(options, proctest.WithEntries(&model.Entry{
		ID: identityActorID, Type: model.TypeSignal, Kind: model.KindActor, Layer: model.LayerProcess, Canonical: "Christopher",
		Content: "Christopher, the known actor role captures bind to.",
	}))...)
	session.LogRead(t, "show", []string{identityActorID}, nil)
	return world, session
}

func TestReasonKinds_GuidedWritesWithoutAReason(t *testing.T) {
	for kind, draft := range reasonFreeDrafts() {
		t.Run(kind, func(t *testing.T) {
			world, session := reasonKindsWorld(t)
			instance := session.Start(t, "capture", nil).Instance
			serve := session.Report(t, instance, draft)
			proctest.RequireStep(t, serve, "playback")
			entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
				return session.Answer(t, instance, "playback", "confirm", nil, "capture it")
			})
			if entry.Kind != model.Kind(kind) || entry.Reason != "" || entry.ReasonBy != "" {
				t.Errorf("the %s should be written without a reason, got kind %q reason %q by %q", kind, entry.Kind, entry.Reason, entry.ReasonBy)
			}
			if wantIntentBy := map[bool]string{true: "Ada"}[kind == "directive"]; entry.IntentBy != wantIntentBy {
				t.Errorf("the confirmed %s should carry intent-by %q, got %q", kind, wantIntentBy, entry.IntentBy)
			}
		})
	}
}

// The agent limit still parks what an agent may not write alone; the reason
// is no longer what holds it.
func TestReasonKinds_AutoNeedsNoReason(t *testing.T) {
	for kind, draft := range reasonFreeDrafts() {
		t.Run(kind, func(t *testing.T) {
			world, session := reasonKindsWorld(t, proctest.WithAutonomousDialogue())
			instance := session.Start(t, "capture", nil).Instance
			serve := session.Report(t, instance, draft)
			if !agentWritable[kind] {
				proctest.RequireStep(t, serve, "agentLimit")
				return
			}
			proctest.RequireStep(t, serve, "playback")
			entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
				return session.Decide(t, instance, "playback", "confirm", nil, "the draft holds")
			})
			if !entry.AgentDecided() || entry.Reason != "" || entry.ReasonBy != "" {
				t.Errorf("the %s should be the agent's without a reason, got decided-by %q reason %q by %q", kind, entry.DecidedBy, entry.Reason, entry.ReasonBy)
			}
		})
	}
}

func TestReasonKinds_AssistedWritesWithoutAReason(t *testing.T) {
	for kind, draft := range reasonFreeDrafts() {
		t.Run(kind, func(t *testing.T) {
			world, session := reasonKindsWorld(t, proctest.WithAssistedDialogue())
			instance := sealedCapture(t, session, draft)
			entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
				return session.Answer(t, instance, "playback", "confirm", nil, "ok")
			})
			if entry.ApprovedBy != "Ada" || entry.Reason != "" || entry.ReasonBy != "" {
				t.Errorf("the approved %s should carry no reason, got approved-by %q reason %q by %q", kind, entry.ApprovedBy, entry.Reason, entry.ReasonBy)
			}
			if wantIntentBy := map[bool]string{true: "Ada"}[kind == "directive"]; entry.IntentBy != wantIntentBy {
				t.Errorf("the approved %s should carry intent-by %q, got %q", kind, wantIntentBy, entry.IntentBy)
			}
		})
	}
}

func TestReasonKinds_AnOptionalReasonIsStored(t *testing.T) {
	const reason = "Christopher makes the strategic calls."
	t.Run("guided", func(t *testing.T) {
		world, session := reasonKindsWorld(t)
		draft := reasonFreeDrafts()["role"]
		draft["reasonWords"] = "he makes the strategic calls"
		instance := session.Start(t, "capture", nil).Instance
		serve := session.Report(t, instance, draft)
		proctest.RequireStep(t, serve, "playback")
		if !strings.Contains(serve.Instructions, "he makes the strategic calls") {
			t.Errorf("the playback should show the user's reason, got:\n%s", serve.Instructions)
		}
		entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
			return session.Answer(t, instance, "playback", "confirm", nil, "capture it")
		})
		if entry.Reason != "he makes the strategic calls" || entry.ReasonBy != "Ada" || entry.IntentBy != "" {
			t.Errorf("the user's reason should be stored as theirs, got %q by %q, intent-by %q", entry.Reason, entry.ReasonBy, entry.IntentBy)
		}
	})
	// A guided directive is written only on the person's confirm, which
	// adopts a proposed reason and intent as theirs, as an approval does.
	t.Run("guided directive", func(t *testing.T) {
		const words, proposal = "empty input hid upstream bugs twice", "Empty input hid bugs upstream."
		for name, tc := range map[string]struct {
			fields     map[string]any
			wantReason string
		}{
			"the user's reason and intent":     {map[string]any{"reasonWords": words, "intentWords": "build it now"}, words},
			"the user's reason alone":          {map[string]any{"reasonWords": words}, words},
			"the user's intent alone":          {map[string]any{"intentWords": "build it now"}, ""},
			"a proposed reason":                {map[string]any{"reason": proposal}, proposal},
			"the user's words over a proposal": {map[string]any{"reason": proposal, "reasonWords": words}, words},
		} {
			t.Run(name, func(t *testing.T) {
				world, session := reasonKindsWorld(t)
				draft := reasonFreeDrafts()["directive"]
				for field, value := range tc.fields {
					draft[field] = value
				}
				instance := session.Start(t, "capture", nil).Instance
				proctest.RequireStep(t, session.Report(t, instance, draft), "playback")
				entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
					return session.Answer(t, instance, "playback", "confirm", nil, "capture it")
				})
				wantReasonBy := map[bool]string{true: "Ada"}[tc.wantReason != ""]
				if entry.Reason != tc.wantReason || entry.ReasonBy != wantReasonBy || entry.IntentBy != "Ada" || entry.ApprovedBy != "" {
					t.Errorf("got reason %q by %q, intent-by %q, approved-by %q; want %q by %q, intent-by Ada, no approval",
						entry.Reason, entry.ReasonBy, entry.IntentBy, entry.ApprovedBy, tc.wantReason, wantReasonBy)
				}
			})
		}
	})
	t.Run("auto", func(t *testing.T) {
		world, session := reasonKindsWorld(t, proctest.WithAutonomousDialogue())
		draft := reasonFreeDrafts()["directive"]
		draft["reason"] = reason
		instance := session.Start(t, "capture", nil).Instance
		proctest.RequireStep(t, session.Report(t, instance, draft), "playback")
		entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
			return session.Decide(t, instance, "playback", "confirm", nil, "the draft holds")
		})
		if entry.Reason != reason || !entry.AgentDecided() || entry.ReasonBy != "" {
			t.Errorf("the agent's reason should be stored without reason-by, got %q by %q decided-by %q", entry.Reason, entry.ReasonBy, entry.DecidedBy)
		}
	})
	t.Run("assisted", func(t *testing.T) {
		world, session := reasonKindsWorld(t, proctest.WithAssistedDialogue())
		draft := reasonFreeDrafts()["role"]
		draft["reason"] = reason
		instance := sealedCapture(t, session, draft)
		entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
			return session.Answer(t, instance, "playback", "confirm", nil, "ok")
		})
		if entry.Reason != reason || entry.ReasonBy != "Ada" {
			t.Errorf("the approved drafted reason should be stored as the person's, got %q by %q", entry.Reason, entry.ReasonBy)
		}
	})
}

// With no participant configured, as when the config loses it after the
// session opened, no one could answer for a guided reason: the capture goes
// on without it, and the playback tells the agent so.
func TestReasonKinds_GuidedReasonWithoutAParticipantIsNamedUnstored(t *testing.T) {
	const unstored = "No participant is configured, so the reason is not stored on the entry: set one with `sdd config set --local participant <name>`."
	world := proctest.NewWorld(t)
	session := world.Open(t, "reason-no-participant")
	draft := decisionDraft("directive")
	draft["reasonWords"] = "empty input hid upstream bugs twice"
	instance := session.Start(t, "capture", nil).Instance
	world.SetParticipant("")
	serve := session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
	if !strings.Contains(serve.Instructions, unstored) {
		t.Errorf("the playback should name the reason unstored, got:\n%s", serve.Instructions)
	}
	entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
		return session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	})
	if entry.Reason != "" || entry.ReasonBy != "" || entry.IntentBy != "" {
		t.Errorf("no reason or marks should be written without a participant, got %q by %q, intent-by %q", entry.Reason, entry.ReasonBy, entry.IntentBy)
	}

	_, session = reasonKindsWorld(t)
	instance = session.Start(t, "capture", nil).Instance
	if serve := session.Report(t, instance, draft); strings.Contains(serve.Instructions, "No participant is configured") {
		t.Errorf("with a participant the playback should name nothing unstored, got:\n%s", serve.Instructions)
	}
}

func planDraft() map[string]any {
	draft := decisionDraft("plan")
	draft["body"] = "Plan: the parser rejects empty input with an explicit error.\n\n## Acceptance criteria\n\n- [ ] Empty input returns an explicit error."
	return draft
}

// A directive still needs its intent value, as stock sdd has it
// (model.DirectiveIntentRequirement), in every dialogue mode.
func TestReasonKinds_ADirectiveStillNeedsItsIntent(t *testing.T) {
	for name, mode := range map[string]proctest.Option{"guided": nil, "auto": proctest.WithAutonomousDialogue(), "assisted": proctest.WithAssistedDialogue()} {
		t.Run(name, func(t *testing.T) {
			var options []proctest.Option
			if mode != nil {
				options = append(options, mode)
			}
			_, session := reasonKindsWorld(t, options...)
			draft := decisionDraft("directive")
			delete(draft, "intent")
			serve := session.Report(t, session.Start(t, "capture", nil).Instance, draft)
			proctest.RequireStep(t, serve, "assemble")
			if !hasDiagnostic(serve, model.DirectiveIntentRequirement) {
				t.Fatalf("a directive without an intent should hold on its intent, got %v", serve.Diagnostics)
			}
		})
	}
}

// The agent may propose a reason and the intent, shown as its inference until
// the user's confirm adopts them.
func TestReasonKinds_GuidedMayProposeAReason(t *testing.T) {
	const propose = "You may propose a reason in `reason` and the intent value, inferred from the dialogue and the graph: the play-back shows them as your inference, the user's confirm adopts them as theirs, and their own words in `reasonWords` take the place of your reason."
	_, session := reasonKindsWorld(t)
	serve := session.Start(t, "capture", nil)
	if !strings.Contains(serve.Instructions, propose) {
		t.Errorf("assemble should let the agent propose a reason, got:\n%s", serve.Instructions)
	}
}

// Auto and assisted infer a reason in the same words: assisted is auto with
// the person's approval.
func TestReasonKinds_AutoAndAssistedInferTheReasonAlike(t *testing.T) {
	const infer = "A decision may state your own reason in `reason`, inferred from the dialogue and the graph; no kind owes one, and a signal carries none. A directive states its intent in `intent`, inferred the same way."
	for name, mode := range agentModes() {
		_, session := reasonKindsWorld(t, mode)
		if serve := session.Start(t, "capture", nil); !strings.Contains(serve.Instructions, infer) {
			t.Errorf("%s assemble should carry the shared inference rule, got:\n%s", name, serve.Instructions)
		}
	}
}

func agentModes() map[string]proctest.Option {
	return map[string]proctest.Option{"auto": proctest.WithAutonomousDialogue(), "assisted": proctest.WithAssistedDialogue()}
}
