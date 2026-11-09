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
// procedure can target.
func newRepeatWorkflow(t *testing.T) (*sdd.WorkflowSession, sdd.RequestIdentity, string) {
	t.Helper()
	graphDir := t.TempDir()
	const id = "20260713-070000-s-tac-tgt"
	path := filepath.Join(graphDir, "2026", "07", "13-070000-s-tac-tgt.md")
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(path, []byte("---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\nsummary: A target for an explore step.\n---\n\n"+repeatTargetBody+"\n"), 0o644)
	if err != nil {
		t.Fatal(err)
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
