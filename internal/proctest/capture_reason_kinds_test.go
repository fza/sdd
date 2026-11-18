package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/test"
)

// Only a directive and a plan owe a reason (model.UserWordsOwed), in every
// dialogue mode; any other decision may carry one, which is then stored.

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
		"activity": decisionDraft("activity"),
	}
}

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
			if entry.Kind != model.Kind(kind) {
				t.Errorf("the %s should be written, got kind %q", kind, entry.Kind)
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
			if kind != "activity" {
				proctest.RequireStep(t, serve, "agentLimit")
				return
			}
			proctest.RequireStep(t, serve, "playback")
			entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
				return session.Decide(t, instance, "playback", "confirm", nil, "the draft holds")
			})
			if !entry.AgentDecided() || entry.Reason != "" {
				t.Errorf("the activity should be the agent's without a reason, got decided-by %q reason %q", entry.DecidedBy, entry.Reason)
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
		})
	}
}

func TestReasonKinds_AnOptionalReasonIsStored(t *testing.T) {
	const reason = "Christopher makes the strategic calls."
	t.Run("guided", func(t *testing.T) {
		_, session := reasonKindsWorld(t)
		draft := reasonFreeDrafts()["role"]
		draft["reasonWords"] = "he makes the strategic calls"
		serve := session.Report(t, session.Start(t, "capture", nil).Instance, draft)
		proctest.RequireStep(t, serve, "playback")
		if !strings.Contains(serve.Instructions, "he makes the strategic calls") {
			t.Errorf("the playback should show the user's reason, got:\n%s", serve.Instructions)
		}
	})
	t.Run("auto", func(t *testing.T) {
		world, session := reasonKindsWorld(t, proctest.WithAutonomousDialogue())
		draft := reasonFreeDrafts()["activity"]
		draft["reason"] = reason
		instance := session.Start(t, "capture", nil).Instance
		proctest.RequireStep(t, session.Report(t, instance, draft), "playback")
		entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
			return session.Decide(t, instance, "playback", "confirm", nil, "the draft holds")
		})
		if entry.Reason != reason || !entry.AgentDecided() {
			t.Errorf("the agent's reason should be stored, got %q decided-by %q", entry.Reason, entry.DecidedBy)
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

func planDraft() map[string]any {
	draft := decisionDraft("plan")
	draft["body"] = "Plan: the parser rejects empty input with an explicit error.\n\n## Acceptance criteria\n\n- [ ] Empty input returns an explicit error."
	return draft
}

func TestReasonKinds_DirectiveAndPlanStillOweAReason(t *testing.T) {
	for _, kind := range []string{"directive", "plan"} {
		draft := func() map[string]any {
			if kind == "plan" {
				return planDraft()
			}
			return decisionDraft(kind)
		}
		t.Run(kind+" guided", func(t *testing.T) {
			_, session := reasonKindsWorld(t)
			serve := session.Report(t, session.Start(t, "capture", nil).Instance, draft())
			proctest.RequireStep(t, serve, "assemble")
			if !hasDiagnostic(serve, "missing reasonWords") {
				t.Fatalf("a %s without the user's reason should hold, got %v", kind, serve.Diagnostics)
			}
		})
		for name, mode := range agentModes() {
			t.Run(kind+" "+name, func(t *testing.T) {
				_, session := reasonKindsWorld(t, mode)
				serve := session.Report(t, session.Start(t, "capture", nil).Instance, draft())
				proctest.RequireStep(t, serve, "assemble")
				if !hasDiagnostic(serve, "states your own reason for it in reason") {
					t.Fatalf("a %s without the agent's reason should hold, got %v", kind, serve.Diagnostics)
				}
			})
		}
	}
}

// Where the user names a reason or an intent, the agent proposes one
// inferred from the dialogue and the graph, put to them as an option beside
// their own.
func TestReasonKinds_GuidedProposesAReasonToPick(t *testing.T) {
	const propose = "put it to the user through the AskUserQuestion tool with two options: your proposal, shown in full, and \"Other: you write your own\""
	_, session := reasonKindsWorld(t)
	serve := session.Start(t, "capture", nil)
	if !strings.Contains(test.Untagged(serve.Instructions), propose) {
		t.Errorf("assemble should tell the agent to propose a reason to pick, got:\n%s", serve.Instructions)
	}
	for _, draft := range []map[string]any{decisionDraft("directive"), planDraft()} {
		held := session.Report(t, session.Start(t, "capture", nil).Instance, draft)
		if !hasDiagnostic(held, propose) {
			t.Errorf("the missing %s reason should send the agent to propose one, got %v", draft["entryKind"], held.Diagnostics)
		}
	}
}

// Auto and assisted infer a directive's and a plan's reason in the same
// words: assisted is auto with the person's approval.
func TestReasonKinds_AutoAndAssistedInferTheReasonAlike(t *testing.T) {
	const infer = "Infer a directive's or a plan's reason from the dialogue and the graph and state it in `reason`, and a directive's intent in `intent`. Any other decision may state one; a signal carries none."
	for name, mode := range agentModes() {
		_, session := reasonKindsWorld(t, mode)
		if serve := session.Start(t, "capture", nil); !strings.Contains(test.Untagged(serve.Instructions), infer) {
			t.Errorf("%s assemble should carry the shared inference rule, got:\n%s", name, serve.Instructions)
		}
	}
}

func agentModes() map[string]proctest.Option {
	return map[string]proctest.Option{"auto": proctest.WithAutonomousDialogue(), "assisted": proctest.WithAssistedDialogue()}
}
