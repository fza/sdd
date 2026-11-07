package gitgate_test

import (
	"context"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/gitgate"
)

func TestLastAttempt(t *testing.T) {
	s := newSetup(t)
	gate := s.gate()
	target := mustTarget(t, "git@gitlab.com:group/project.git")
	other := mustTarget(t, "git@gitlab.com:group/other.git")

	last, err := gate.LastAttempt(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	if !last.IsZero() {
		t.Errorf("a repository the gate never admitted should have no last attempt, got %s", last)
	}

	runs := 0
	if err := gate.Run(t.Context(), target, gitgate.Fetch, sshOp(t, &runs)); err != nil {
		t.Fatal(err)
	}
	pushedAt := t0.Add(time.Hour)
	s.clock.Set(pushedAt)
	if err := gate.Run(t.Context(), target, gitgate.Push, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}

	last, err = gate.LastAttempt(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	if !last.Equal(pushedAt) {
		t.Errorf("the last attempt should be the later of fetch and push, %s, got %s", pushedAt, last)
	}
	last, err = gate.LastAttempt(t.Context(), other)
	if err != nil {
		t.Fatal(err)
	}
	if !last.IsZero() {
		t.Errorf("another repository's attempts should not count, got %s", last)
	}
}
