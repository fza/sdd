package application_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

const (
	repeatTargetBody = "The target names the work an explore step serves in full."
	repeatTargetFile = "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\nsummary: A target for an explore step.\n---\n\n" + repeatTargetBody + "\n"
)

// targetHash is the target entry's model.EntryHash, as the session's graph
// parses it.
func targetHash(t *testing.T) string {
	t.Helper()
	e, err := model.ParseEntry("20260713-070000-s-tac-tgt.md", repeatTargetFile)
	if err != nil {
		t.Fatal(err)
	}
	return model.EntryHash(e)
}

// newRepeatWorkflow opens a session over a graph holding one gap the explore
// procedure can target, and the extra entries, keyed by file name under
// 2026/07.
func newRepeatWorkflow(t *testing.T, extra ...map[string]string) (*sdd.WorkflowSession, sdd.RequestIdentity, string) {
	t.Helper()
	graphDir := t.TempDir()
	const id = "20260713-070000-s-tac-tgt"
	files := map[string]string{
		"13-070000-s-tac-tgt.md": repeatTargetFile,
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

// holdEntry opens a call that serves the entry in full, so the rest of the
// call holds it.
func holdEntry(t *testing.T, workflow *sdd.WorkflowSession, id string) {
	t.Helper()
	workflow.BeginCall("", nil)
	workflow.ServeEntry(id, targetHash(t), repeatTargetBody)
}

func heldPointer(id string) string {
	return id + " — A target for an explore step. (served in full above"
}

// The no-repeat rule cuts an injected entry chain only once CutRepeats turned
// it on, as the MCP surface does; the CLI never cuts.
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
			holdEntry(t, workflow, id)
			serve, err := workflow.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "explore", Params: map[string]any{"targets": []any{id}, "goal": "the target"}})
			if err != nil {
				t.Fatal(err)
			}
			pointed := strings.Contains(serve.Instructions, heldPointer(id))
			if pointed != tc.pointer || strings.Contains(serve.Instructions, repeatTargetBody) == tc.pointer {
				t.Fatalf("a held target should print as its pointer only with cuts on:\n%s", serve.Instructions)
			}
		})
	}
}

// With cuts on, an entry an injected chain serves in full is held for the
// rest of its call only: a later call gets it in full again whatever it
// answers (20261121-230157-d-tac-ym8).
func TestInjectedEntryChainsHoldWhatTheyServeForTheCallOnly(t *testing.T) {
	workflow, identity, id := newRepeatWorkflow(t)
	workflow.CutRepeats()
	workflow.BeginCall("", nil)
	serve, err := workflow.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "explore", Params: map[string]any{"targets": []any{id}, "goal": "the target"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(serve.Instructions, repeatTargetBody) {
		t.Fatalf("the target should serve in full:\n%s", serve.Instructions)
	}
	if !workflow.HoldsEntry(id, targetHash(t)) {
		t.Fatal("the call that served the target should hold it")
	}
	err = workflow.RecordPieces(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	workflow.BeginCall("anything", nil)
	if workflow.HoldsEntry(id, targetHash(t)) {
		t.Error("a later call should get the target in full again")
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
		holdEntry(t, workflow, id)
	}
	serve, err := workflow.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "view-step"})
	if err != nil {
		t.Fatal(err)
	}
	return workflow, id, serve.Instructions
}

// A view a step injects serves a held entry as its pointer, in a bodies
// section and in a listing alike, only once CutRepeats turned the rule on.
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
			pointers := strings.Count(instructions, heldPointer(id))
			if tc.pointer && pointers != 2 || !tc.pointer && pointers != 0 {
				t.Fatalf("both views should print the held target as its pointer only with cuts on:\n%s", instructions)
			}
			if strings.Contains(instructions, repeatTargetBody) == tc.pointer {
				t.Fatalf("the bodies view should serve the held target's body only with cuts off:\n%s", instructions)
			}
		})
	}
}

// With cuts on, a step's injected view records the entries it shows in full
// as served within the call; with cuts off it records none.
func TestInjectedViewRecordsWhatItServedInFull(t *testing.T) {
	for _, cut := range []bool{true, false} {
		workflow, id, instructions := startViewStep(t, cut, false)
		if !strings.Contains(instructions, repeatTargetBody) {
			t.Fatalf("the step should serve the target in full:\n%s", instructions)
		}
		if held := workflow.HoldsEntry(id, targetHash(t)); held != cut {
			t.Fatalf("the target served in full should count as served within the call exactly with cuts on, cuts=%v", cut)
		}
	}
}

// Framing lanes never cut, with cuts on too: a held entry a lane serves still
// prints in full there, so the per-lane dedup stays byte-stable.
func TestFramingServesHeldEntriesInFull(t *testing.T) {
	const principlesFactID = "20260810-190000-s-prc-way"
	workflow, identity, _ := newRepeatWorkflow(t)
	workflow.CutRepeats()
	holdEntry(t, workflow, principlesFactID)
	blocks, err := workflow.Framing(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	framing := strings.Join(blocks, "\n\n")
	if !strings.Contains(framing, "## Working principles") || strings.Contains(framing, "(held as") {
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

// A mixed view counts the bodies it served as served only when the step
// serves it uncut: a line cut can drop a body the view rendered.
func TestInjectedMixedViewHoldsOnlyWhenUncut(t *testing.T) {
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
			if held := workflow.HoldsEntry(id, targetHash(t)); held != tc.held {
				t.Fatalf("the target should count as served only when the step served the view uncut, held=%v", held)
			}
		})
	}
}

// A resume that leaves an instance out for size withdraws what it served, so
// a later instance serves those entries in full rather than as pointers to
// text the agent never got.
func TestServeAllWithinWithdrawsWhatAnOmittedInstanceServed(t *testing.T) {
	workflow, identity, id := newRepeatWorkflow(t)
	workflow.CutRepeats()
	var explores []string
	for range 2 {
		serve, err := workflow.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "explore", Params: map[string]any{"targets": []any{id}, "goal": "the target"}})
		if err != nil {
			t.Fatal(err)
		}
		explores = append(explores, serve.Instance)
	}
	workflow.BeginCall("", nil)
	if err := workflow.Reorient(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	var kept *sdd.WorkflowServe
	_, err := workflow.ServeAllWithin(t.Context(), identity, func(_ sdd.WorkflowResumeResult, serve sdd.WorkflowServe) (string, error) {
		if serve.Instance == explores[0] {
			return "", nil
		}
		if serve.Instance == explores[1] {
			kept = &serve
		}
		return serve.Instructions, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if kept == nil {
		t.Fatal("the second explore should be delivered")
	}
	if strings.Contains(kept.Instructions, "(served in full") || !strings.Contains(kept.Instructions, repeatTargetBody) {
		t.Fatalf("the kept explore should serve the target in full:\n%s", kept.Instructions)
	}
}

// chainStepProcedure injects the repeat target's chain, bounded by bound (an
// inject field, or empty).
func chainStepProcedure(bound string) string {
	return `---
type: decision
kind: procedure
layer: process
canonical: chain-step
confidence: high
summary: A workflow whose step serves the repeat target's chain.
state:
    body: {type: text, desc: what the chain showed}
steps:
    - id: look
      inject:
          - {fn: entryChains, args: {id: "20260713-070000-s-tac-tgt"}` + bound + `}
      collect: [body]
      transitions:
          - when: hasBody
            to: end(completed)
---

Serves the repeat target's chain.

## unit: look

{{.entryChains}}
`
}

// An injected chain counts the entries it served as served only when the step
// serves it uncut, as a view does.
func TestInjectedEntryChainsHoldOnlyWhenUncut(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bound string
		held  bool
	}{
		{"uncut", "", true},
		{"cut", ", maxBytes: 60", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, identity, id := newRepeatWorkflow(t, map[string]string{"13-070003-d-prc-chs.md": chainStepProcedure(tc.bound)})
			workflow.CutRepeats()
			serve, err := workflow.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "chain-step"})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(serve.Instructions, repeatTargetBody) != tc.held {
				t.Fatalf("the step should serve the body whole only uncut:\n%s", serve.Instructions)
			}
			if held := workflow.HoldsEntry(id, targetHash(t)); held != tc.held {
				t.Fatalf("the target should count as served only when the step served the chain uncut, held=%v", held)
			}
		})
	}
}
