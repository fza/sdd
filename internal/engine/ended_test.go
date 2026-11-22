package engine_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
)

func endedMoves(sess *engine.Session) []string {
	var moves []string
	for _, inst := range sess.EndedInstances() {
		moves = append(moves, inst.ID+" "+inst.Outcome)
	}
	return moves
}

// Ended instances list with their outcome in the order they ended, not the
// order they started, and replay restores that order from the log.
func TestEndedInstancesFollowTheOrderTheyEnded(t *testing.T) {
	spec := patchSpec(t)
	sink := &recordingSink{}
	sess := newPatchEngine().NewSession("s_ended", "tester", sink)
	var started []string
	for range 3 {
		sv, err := sess.Start(spec, map[string]any{"body": "alpha"}, "")
		if err != nil {
			t.Fatal(err)
		}
		started = append(started, sv.Instance)
	}
	if ended := endedMoves(sess); len(ended) != 0 {
		t.Fatalf("no instance should list as ended while all run, got %v", ended)
	}

	if err := sess.Abandon(started[2], "dropped"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Report(started[0], map[string]any{"goal": "done"}); err != nil {
		t.Fatal(err)
	}

	want := []string{started[2] + " abandoned", started[0] + " completed"}
	if got := endedMoves(sess); !slices.Equal(got, want) {
		t.Errorf("ended instances should follow the order they ended: want %v, got %v", want, got)
	}
	resolve := func(string) (*engine.Spec, error) { return spec, nil }
	replayed, err := newPatchEngine().ReplaySession("s_ended", "tester", sink.events, resolve, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := endedMoves(replayed); !slices.Equal(got, want) {
		t.Errorf("replay should restore the order instances ended in: want %v, got %v", want, got)
	}
}
