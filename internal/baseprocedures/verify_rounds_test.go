package baseprocedures_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/baseprocedures"
	"github.com/networkteam/sdd/internal/engine"
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

// A claim naming a branch, tag or commit holds at that revision, while the
// checkout holds the working tree, so the verifier's rules send it to read_git.
func TestVerifyRulesCheckARevisionClaimAtItsRevision(t *testing.T) {
	entries, err := baseprocedures.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Canonical != "verify" {
			continue
		}
		spec, err := engine.ParseSpec(entry)
		if err != nil {
			t.Fatal(err)
		}
		for _, lane := range spec.Units["page"].Lanes {
			if lane.Name != "rules" {
				continue
			}
			if !strings.Contains(lane.Text, "A claim that names a branch, tag or commit") || !strings.Contains(lane.Text, "read the file there through `read_git`") {
				t.Fatalf("the verifier's rules should send a claim naming a revision to read_git at that revision, got:\n%s", lane.Text)
			}
			return
		}
		t.Fatal("the verify page unit should carry a rules lane")
	}
	t.Fatal("the base procedures should carry verify")
}
