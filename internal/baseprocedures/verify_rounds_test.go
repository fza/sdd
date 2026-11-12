package baseprocedures_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/baseprocedures"
	"github.com/networkteam/sdd/internal/model"
)

// The verify task's round cap covers three deliveries for its first exchange
// and for each revision it takes.
func TestVerifyRoundCapCoversEveryRevision(t *testing.T) {
	entries, err := baseprocedures.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Canonical != "verify" {
			continue
		}
		if entry.ProcedureSpec.Rounds != model.DeepRounds*(1+model.DeepRevisions) {
			t.Fatalf("the verify round cap should cover every exchange it may hold, got %d", entry.ProcedureSpec.Rounds)
		}
		return
	}
	t.Fatal("the base procedures should carry verify")
}
