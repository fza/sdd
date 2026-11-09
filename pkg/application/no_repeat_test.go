package application_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
)

const repeatTargetBody = "The target names the work an explore step serves in full."

// newRepeatWorkflow opens a session over a graph holding one gap the explore
// procedure can target, and the extra entries, keyed by file name under
// 2026/07.
func newRepeatWorkflow(t *testing.T, extra ...map[string]string) (*sdd.WorkflowSession, sdd.RequestIdentity, string) {
	t.Helper()
	graphDir := t.TempDir()
	const id = "20260713-070000-s-tac-tgt"
	files := map[string]string{
		"13-070000-s-tac-tgt.md": "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\nsummary: A target for an explore step.\n---\n\n" + repeatTargetBody + "\n",
	}
	for _, more := range extra {
		for name, content := range more {
			files[name] = content
		}
	}
	err := os.MkdirAll(filepath.Join(graphDir, "2026", "07"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		err = os.WriteFile(filepath.Join(graphDir, "2026", "07", name), []byte(content), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	application, _, _, _ := newStampWorkflowApp(t, graphDir, "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	workflow, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "client"})
	if err != nil {
		t.Fatal(err)
	}
	return workflow, identity, id
}

// The no-repeat rule cuts an injected entry chain only once CutRepeats turned
// it on, as the MCP surface does; the CLI never cuts (R14).
func TestInjectedEntryChainsCutRepeatsOnlyWhenTurnedOn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cut     bool
		pointer bool
	}{
		{"cuts off", false, false},
		{"cuts on", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, identity, id := newRepeatWorkflow(t)
			if tc.cut {
				workflow.CutRepeats()
			}
			err := workflow.RecordServed(t.Context(), identity, []string{sdd.HeldEntryHash(id)})
			if err != nil {
				t.Fatal(err)
			}
			serve, err := workflow.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "explore", Params: map[string]any{"targets": []any{id}, "goal": "the target"}})
			if err != nil {
				t.Fatal(err)
			}
			pointed := strings.Contains(serve.Instructions, id+" — A target for an explore step. (served in full above)")
			if pointed != tc.pointer || strings.Contains(serve.Instructions, repeatTargetBody) == tc.pointer {
				t.Fatalf("a held target should print as its pointer only with cuts on:\n%s", serve.Instructions)
			}
		})
	}
}

// With cuts on, an entry an injected chain serves in full is held from then on.
func TestInjectedEntryChainsHoldWhatTheyServe(t *testing.T) {
	workflow, identity, id := newRepeatWorkflow(t)
	workflow.CutRepeats()
	_, err := workflow.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "explore", Params: map[string]any{"targets": []any{id}, "goal": "the target"}})
	if err != nil {
		t.Fatal(err)
	}
	if !workflow.ServedBefore(sdd.HeldEntryHash(id)) {
		t.Fatal("the target the step served in full should be held")
	}
}

// viewStepProcedure injects the repeat target's body and a gap listing into
// its one step through viewLayout.
const viewStepProcedure = `---
type: decision
kind: procedure
layer: process
canonical: view-step
confidence: high
summary: A workflow whose step serves views of the repeat target.
state:
    body: {type: text, desc: what the views showed}
steps:
    - id: look
      inject:
          - {id: bodies, fn: viewLayout, args: {layout: 'id("20260713-070000-s-tac-tgt"):as-bodies'}}
          - {id: listed, fn: viewLayout, args: {layout: 'kind(gap):as-list'}}
      collect: [body]
      transitions:
          - when: hasBody
            to: end(completed)
---

Serves views of the repeat target.

## unit: look

{{.bodies}}

{{.listed}}
`

func startViewStep(t *testing.T, cut bool, held bool) (*sdd.WorkflowSession, string, string) {
	t.Helper()
	workflow, identity, id := newRepeatWorkflow(t, map[string]string{"13-070001-d-prc-vws.md": viewStepProcedure})
	if cut {
		workflow.CutRepeats()
	}
	if held {
		err := workflow.RecordServed(t.Context(), identity, []string{sdd.HeldEntryHash(id)})
		if err != nil {
			t.Fatal(err)
		}
	}
	serve, err := workflow.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "view-step"})
	if err != nil {
		t.Fatal(err)
	}
	return workflow, id, serve.Instructions
}

// A view a step injects serves a held entry as its pointer, in a bodies
// section and in a listing alike, only once CutRepeats turned the rule on
// (R14).
func TestInjectedViewCutRepeatsOnlyWhenTurnedOn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cut     bool
		pointer bool
	}{
		{"cuts off", false, false},
		{"cuts on", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, id, instructions := startViewStep(t, tc.cut, true)
			pointers := strings.Count(instructions, id+" — A target for an explore step. (served in full above)")
			if tc.pointer && pointers != 2 || !tc.pointer && pointers != 0 {
				t.Fatalf("both views should print the held target as its pointer only with cuts on:\n%s", instructions)
			}
			if strings.Contains(instructions, repeatTargetBody) == tc.pointer {
				t.Fatalf("the bodies view should serve the held target's body only with cuts off:\n%s", instructions)
			}
		})
	}
}

// With cuts on, a step's injected view hands on the entries it served in
// full, once; with cuts off it hands on none.
func TestInjectedViewHandsOnWhatItServedInFull(t *testing.T) {
	t.Run("cuts on", func(t *testing.T) {
		workflow, id, instructions := startViewStep(t, true, false)
		if !strings.Contains(instructions, repeatTargetBody) {
			t.Fatalf("the step should serve the target in full:\n%s", instructions)
		}
		if got := workflow.TakeStepFullIDs(); len(got) != 1 || got[0] != id {
			t.Fatalf("the step should hand on the target it served in full, got %v", got)
		}
		if got := workflow.TakeStepFullIDs(); len(got) != 0 {
			t.Fatalf("a handed-on entry should be forgotten, got %v", got)
		}
	})
	t.Run("cuts off", func(t *testing.T) {
		workflow, _, _ := startViewStep(t, false, false)
		if got := workflow.TakeStepFullIDs(); len(got) != 0 {
			t.Fatalf("without cuts the step should hand on nothing, got %v", got)
		}
	})
}

// Framing lanes never cut, with cuts on too: a held entry a lane serves still
// prints in full there, so the per-lane dedup stays byte-stable.
func TestFramingServesHeldEntriesInFull(t *testing.T) {
	const principlesFactID = "20260810-190000-s-prc-way"
	workflow, identity, _ := newRepeatWorkflow(t)
	workflow.CutRepeats()
	err := workflow.RecordServed(t.Context(), identity, []string{sdd.HeldEntryHash(principlesFactID)})
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := workflow.Framing(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	framing := strings.Join(blocks, "\n\n")
	if !strings.Contains(framing, "## Working principles") || strings.Contains(framing, "(served in full above)") {
		t.Fatalf("the framing should serve the held principles in full:\n%s", framing)
	}
	if got := workflow.TakeStepFullIDs(); len(got) != 0 {
		t.Fatalf("framing lanes should hand on nothing as step serves, got %v", got)
	}
}

// mixedViewProcedure injects a view of the repeat target's body beside a gap
// listing, bounded by bound (an inject field, or empty).
func mixedViewProcedure(canonical, bound string) string {
	return `---
type: decision
kind: procedure
layer: process
canonical: ` + canonical + `
confidence: high
summary: A workflow whose step serves a mixed view of the repeat target.
state:
    body: {type: text, desc: what the view showed}
steps:
    - id: look
      inject:
          - {fn: viewLayout, args: {layout: 'id("20260713-070000-s-tac-tgt"):as-bodies,kind(gap):as-list'}` + bound + `}
      collect: [body]
      transitions:
          - when: hasBody
            to: end(completed)
---

Serves a mixed view of the repeat target.

## unit: look

{{.viewLayout}}
`
}

// A mixed view hands on the bodies it served only when the step serves it
// uncut: a line cut can drop a body the view rendered.
func TestInjectedMixedViewHandsOnOnlyWhenUncut(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bound string
		held  bool
	}{
		{"uncut", "", true},
		{"cut", ", maxBytes: 60", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, identity, id := newRepeatWorkflow(t, map[string]string{"13-070002-d-prc-vwm.md": mixedViewProcedure("view-mixed", tc.bound)})
			workflow.CutRepeats()
			serve, err := workflow.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "view-mixed"})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(serve.Instructions, repeatTargetBody) != tc.held {
				t.Fatalf("the step should serve the body whole only uncut:\n%s", serve.Instructions)
			}
			got := workflow.TakeStepFullIDs()
			if handed := len(got) == 1 && got[0] == id; handed != tc.held || !tc.held && len(got) != 0 {
				t.Fatalf("the step should hand on the target only when it served the view uncut, got %v", got)
			}
		})
	}
}
