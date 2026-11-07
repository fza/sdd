package test

import (
	"embed"
	"testing"
)

//go:embed fixtures/forcepush
var forcePushFixtures embed.FS

// Entries of the force-push fixtures, by repository path: a decision, an
// entry refining it, a plain new signal, and an entry superseding the
// decision whose bare ref fails the mechanical pre-flight.
const (
	ForcePushDecision  = ".sdd/graph/2026/11/07-110000-d-tac-dcs.md"
	ForcePushRefines   = ".sdd/graph/2026/11/07-120500-d-tac-rfn.md"
	ForcePushPlain     = ".sdd/graph/2026/11/07-121000-s-tac-pln.md"
	ForcePushUnchecked = ".sdd/graph/2026/11/07-121500-d-tac-unc.md"
)

// ForcePushFixture returns the content of the named force-push fixture.
func ForcePushFixture(t *testing.T, name string) string {
	t.Helper()
	content, err := forcePushFixtures.ReadFile("fixtures/forcepush/" + name)
	if err != nil {
		t.Fatalf("reading force-push fixture %s: %v", name, err)
	}
	return string(content)
}
