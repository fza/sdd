package application_test

import (
	"context"
	"strings"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// The signatures programs built on the upstream packages call; a change to
// one of them fails the build here.
var (
	_ func(*sdd.WorkflowServe) string                                                  = (*sdd.WorkflowServe).ReminderInstructions
	_ func(*sdd.WorkflowSession, string) bool                                          = (*sdd.WorkflowSession).ServedBefore
	_ func(*sdd.WorkflowSession, context.Context, sdd.RequestIdentity, []string) error = (*sdd.WorkflowSession).RecordServed
	_                                                                                  = sdd.Finding{} == sdd.Finding{}
)

func TestReminderInstructionsNameTheStepAndKeepDiagnostics(t *testing.T) {
	serve := &sdd.WorkflowServe{Step: "playback", Goal: "confirm the draft", Diagnostics: []string{"missing body"}}
	got := serve.ReminderInstructions()
	for _, want := range []string{"step playback", "confirm the draft", "resume_session", "missing body"} {
		if !strings.Contains(got, want) {
			t.Errorf("the reminder should carry %q, got %q", want, got)
		}
	}
	if (*sdd.WorkflowServe)(nil).ReminderInstructions() != "" {
		t.Error("a nil serve should remind of nothing")
	}
}

func TestFindingBasisIDs(t *testing.T) {
	if (sdd.Finding{}).BasisIDs() != nil {
		t.Error("a finding without basis should name none")
	}
	basis := []string{"20260101-100000-s-tac-aaa"}
	if got := (sdd.Finding{Basis: &basis}).BasisIDs(); len(got) != 1 || got[0] != basis[0] {
		t.Errorf("BasisIDs = %v, want %v", got, basis)
	}
}
