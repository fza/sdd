package application_test

import (
	"strings"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// Deep verification needs Claude Code in a connected repo too: a Codex client
// can neither start a capture into a dependency that checks deep nor resume a
// session whose running capture targets one.
func TestCodexClientIsRefusedADeepDependency(t *testing.T) {
	application, alice := newDependencyFixture(t, map[sdd.ProjectID]sdd.VerifierPolicy{"project-b": {Mode: "deep"}})
	const refusal = "verify.mode deep needs Claude Code"

	codex, _, err := application.OpenWorkflow(t.Context(), alice, "project-a", sdd.WorkflowOpenRequest{ClientName: "codex-mcp-client"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codex.Start(t.Context(), alice, sdd.WorkflowStartRequest{Canonical: "capture", Project: "project-b"}); err == nil || !strings.Contains(err.Error(), refusal) {
		t.Fatalf("a Codex capture into a deep dependency should be refused naming the setting, got %v", err)
	}

	claude, _, err := application.OpenWorkflow(t.Context(), alice, "project-a", sdd.WorkflowOpenRequest{ClientName: "claude-code"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := claude.Start(t.Context(), alice, sdd.WorkflowStartRequest{Canonical: "capture", Project: "project-b"}); err != nil {
		t.Fatalf("a Claude Code capture into a deep dependency should start: %v", err)
	}
	if err := application.RefuseClient(t.Context(), alice, claude.ID(), "codex-mcp-client"); err == nil || !strings.Contains(err.Error(), refusal) {
		t.Fatalf("a Codex resume of the session should be refused naming the setting, got %v", err)
	}
	if _, err := application.LoadWorkflow(t.Context(), alice, sdd.WorkflowResumeRequest{SessionID: claude.ID(), ClientName: "codex-mcp-client"}); err == nil || !strings.Contains(err.Error(), refusal) {
		t.Fatalf("a Codex load of the session should be refused naming the setting, got %v", err)
	}
}
