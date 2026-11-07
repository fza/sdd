package syncstate_test

import (
	"errors"
	"testing"

	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/syncstate"
)

func TestOwnedClonesLastAttemptNeedsTheGate(t *testing.T) {
	root := graphRepo(t)
	run(t, root, "remote", "add", "origin", "git@github.com:acme/graph.git")
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("reading the last attempt without the gate should fail with ErrNoGate, panicked: %v", recovered)
		}
	}()
	if _, err := (syncstate.OwnedClones{}).LastAttempt(t.Context(), root); !errors.Is(err, repos.ErrNoGate) {
		t.Errorf("reading the last attempt without the gate should fail with ErrNoGate, got %v", err)
	}
}
