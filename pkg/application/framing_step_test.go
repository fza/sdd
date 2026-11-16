package application_test

import (
	"slices"
	"strings"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
)

// framedStepProcedure is a move whose one step declares the framing blocks
// it carries.
func framedStepProcedure(canonical, framing string) string {
	return `---
type: decision
kind: procedure
layer: process
canonical: ` + canonical + `
confidence: high
summary: A workflow whose step declares the framing it carries.
state:
    body: {type: text, desc: what the step reports}
steps:
    - id: look
      framing: ` + framing + `
      collect: [body]
      transitions:
          - when: hasBody
            to: end(completed)
---

Declares its framing.

## unit: look

Report the body.
`
}

func blockNames(blocks []types.ServeLane) []string {
	names := make([]string, 0, len(blocks))
	for _, block := range blocks {
		names = append(names, block.Name)
	}
	return names
}

// A step declaring framing blocks is served those alone; the shell's
// other lanes stay out.
func TestStepFramingServesTheDeclaredBlocks(t *testing.T) {
	workflow, identity, _ := newRepeatWorkflow(t, map[string]string{"13-070001-d-prc-frm.md": framedStepProcedure("framed-step", "[info, recentMovement]")})
	all, err := workflow.FramingBlocks(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(blockNames(all), "principles") {
		t.Fatalf("the shell should frame its principles, got %v", blockNames(all))
	}
	serve, err := workflow.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "framed-step"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(serve.Framing, []string{"info", "recentMovement"}) {
		t.Fatalf("the serve should carry the step's declared framing, got %v", serve.Framing)
	}
	blocks, err := workflow.FramingBlocksFor(t.Context(), identity, serve.Framing)
	if err != nil {
		t.Fatal(err)
	}
	if got := blockNames(blocks); !slices.Equal(got, []string{"info", "recentMovement"}) {
		t.Fatalf("the step should be framed by its declared blocks alone, got %v", got)
	}
}

// A step naming a block the session does not frame fails the framing loud.
func TestStepFramingRefusesABlockTheSessionDoesNotFrame(t *testing.T) {
	workflow, identity, _ := newRepeatWorkflow(t, map[string]string{"13-070001-d-prc-frm.md": framedStepProcedure("framed-step", "[info, weather]")})
	serve, err := workflow.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "framed-step"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = workflow.FramingBlocksFor(t.Context(), identity, serve.Framing)
	if err == nil || !strings.Contains(err.Error(), `step framing names "weather"`) {
		t.Fatalf("a block the session does not frame should fail the framing, got %v", err)
	}
}
